package files

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/promptcompat"
)

const (
	maxInlineFilesPerRequest = 50
	// Limits aligned with DeepSeek vision docs:
	// https://api-docs.deepseek.com/zh-cn/guides/vision
	maxRemoteImageURLLen     = 8192
	maxRemoteImageBytes      = 32 << 20 // 32 MiB
	remoteImageFetchTimeout  = 60 * time.Second
)

type inlineFileUploadError struct {
	status  int
	message string
	err     error
}

func (e *inlineFileUploadError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.message) != "" {
		return e.message
	}
	if e.err != nil {
		return e.err.Error()
	}
	return "inline file processing failed"
}

type inlineUploadState struct {
	ctx             context.Context
	handler         *Handler
	auth            *auth.RequestAuth
	modelType       string
	uploadedByID    map[string]string
	uploadCount     int
	inlineFileBytes int
}

type inlineDecodedFile struct {
	Data            []byte
	ContentType     string
	Filename        string
	ReplacementType string
	RemoteURL       string
}

func (h *Handler) PreprocessInlineFileInputs(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	if h == nil || h.DS == nil || len(req) == 0 {
		return nil
	}
	modelType := "default"
	if requestedModel, ok := req["model"].(string); ok {
		if resolvedModel, ok := config.ResolveModel(requestedModel); ok {
			if resolvedType, ok := config.GetModelType(config.UpstreamDeepSeekSKU(resolvedModel)); ok {
				modelType = config.UpstreamSafeModelType(resolvedType)
			}
		}
	}
	state := &inlineUploadState{
		ctx:          ctx,
		handler:      h,
		auth:         a,
		modelType:    modelType,
		uploadedByID: map[string]string{},
	}
	for _, key := range []string{"messages", "input", "attachments"} {
		if raw, ok := req[key]; ok {
			updated, err := state.walk(raw)
			if err != nil {
				return err
			}
			req[key] = updated
		}
	}
	if refIDs := promptcompat.CollectOpenAIRefFileIDs(req); len(refIDs) > 0 {
		req["ref_file_ids"] = stringsToAnySlice(refIDs)
	}
	if state.inlineFileBytes > 0 {
		req["_inline_file_bytes"] = state.inlineFileBytes
	}
	return nil
}

func WriteInlineFileError(w http.ResponseWriter, err error) {
	inlineErr, ok := err.(*inlineFileUploadError)
	if !ok || inlineErr == nil {
		shared.WriteOpenAIError(w, http.StatusInternalServerError, "Failed to process file input.")
		return
	}
	status := inlineErr.status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	message := strings.TrimSpace(inlineErr.message)
	if message == "" {
		message = "Failed to process file input."
	}
	shared.WriteOpenAIError(w, status, message)
}

func (s *inlineUploadState) walk(raw any) (any, error) {
	switch x := raw.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			updated, err := s.walk(item)
			if err != nil {
				return nil, err
			}
			out[i] = updated
		}
		return out, nil
	case map[string]any:
		if replacement, replaced, err := s.tryUploadBlock(x); replaced || err != nil {
			return replacement, err
		}
		for _, key := range []string{"messages", "input", "attachments", "content", "files", "items", "data", "file", "image_url"} {
			if nested, ok := x[key]; ok {
				updated, err := s.walk(nested)
				if err != nil {
					return nil, err
				}
				x[key] = updated
			}
		}
		return x, nil
	default:
		return raw, nil
	}
}

func (s *inlineUploadState) tryUploadBlock(block map[string]any) (map[string]any, bool, error) {
	decoded, ok, err := decodeOpenAIInlineFileBlock(block)
	if err != nil {
		return nil, true, &inlineFileUploadError{status: http.StatusBadRequest, message: err.Error(), err: err}
	}
	if !ok {
		return nil, false, nil
	}
	if remote := strings.TrimSpace(decoded.RemoteURL); remote != "" {
		data, contentType, fetchErr := fetchRemoteImage(s.ctx, remote)
		if fetchErr != nil {
			return nil, true, &inlineFileUploadError{status: http.StatusBadRequest, message: fetchErr.Error(), err: fetchErr}
		}
		decoded.Data = data
		if strings.TrimSpace(decoded.ContentType) == "" {
			decoded.ContentType = contentType
		}
		if strings.TrimSpace(decoded.Filename) == "" {
			decoded.Filename = pickInlineFilename(block, decoded.ContentType, "image")
		}
		decoded.RemoteURL = ""
	}
	if len(decoded.Data) == 0 {
		err := fmt.Errorf("empty image payload")
		return nil, true, &inlineFileUploadError{status: http.StatusBadRequest, message: err.Error(), err: err}
	}
	if s.uploadCount >= maxInlineFilesPerRequest {
		err := fmt.Errorf("exceeded maximum of %d inline files per request", maxInlineFilesPerRequest)
		return nil, true, &inlineFileUploadError{status: http.StatusBadRequest, message: err.Error(), err: err}
	}
	fileID, err := s.uploadInlineFile(decoded)
	if err != nil {
		return nil, true, &inlineFileUploadError{status: http.StatusInternalServerError, message: "Failed to upload inline file.", err: err}
	}
	s.uploadCount++
	s.inlineFileBytes += len(decoded.Data)
	replacement := map[string]any{
		"type":    decoded.ReplacementType,
		"file_id": fileID,
	}
	if decoded.Filename != "" {
		replacement["filename"] = decoded.Filename
	}
	if decoded.ContentType != "" {
		replacement["mime_type"] = decoded.ContentType
	}
	return replacement, true, nil
}

func (s *inlineUploadState) uploadInlineFile(file inlineDecodedFile) (string, error) {
	sum := sha256.Sum256(append([]byte(file.ContentType+"\x00"+file.Filename+"\x00"), file.Data...))
	cacheKey := fmt.Sprintf("%x", sum[:])
	if fileID, ok := s.uploadedByID[cacheKey]; ok && strings.TrimSpace(fileID) != "" {
		return fileID, nil
	}
	contentType := strings.TrimSpace(file.ContentType)
	if contentType == "" {
		contentType = http.DetectContentType(file.Data)
	}
	result, err := s.handler.DS.UploadFile(s.ctx, s.auth, dsclient.UploadFileRequest{
		Filename:    file.Filename,
		ContentType: contentType,
		ModelType:   s.modelType,
		Data:        file.Data,
	}, 3)
	if err != nil {
		return "", err
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return "", fmt.Errorf("upload succeeded without file id")
	}
	s.uploadedByID[cacheKey] = fileID
	return fileID, nil
}

func decodeOpenAIInlineFileBlock(block map[string]any) (inlineDecodedFile, bool, error) {
	if block == nil {
		return inlineDecodedFile{}, false, nil
	}
	// Already uploaded / Files API reference — leave alone.
	if strings.TrimSpace(shared.AsString(block["file_id"])) != "" {
		return inlineDecodedFile{}, false, nil
	}
	if nested, ok := block["file"].(map[string]any); ok {
		decoded, matched, err := decodeOpenAIInlineFileBlock(nested)
		if err != nil || !matched {
			return decoded, matched, err
		}
		if decoded.Filename == "" {
			decoded.Filename = pickInlineFilename(block, decoded.ContentType, defaultInlinePrefix(decoded.ReplacementType))
		}
		return decoded, true, nil
	}

	blockType := strings.ToLower(strings.TrimSpace(shared.AsString(block["type"])))
	if payload, remoteURL, matched := extractInlineImagePayload(block, blockType); matched {
		if remoteURL != "" {
			return inlineDecodedFile{
				RemoteURL:       remoteURL,
				ContentType:     contentTypeFromMap(block),
				Filename:        pickInlineFilename(block, contentTypeFromMap(block), "image"),
				ReplacementType: "input_image",
			}, true, nil
		}
		data, contentType, err := decodeInlinePayload(payload, contentTypeFromMap(block))
		if err != nil {
			return inlineDecodedFile{}, true, fmt.Errorf("invalid image input")
		}
		return inlineDecodedFile{
			Data:            data,
			ContentType:     contentType,
			Filename:        pickInlineFilename(block, contentType, "image"),
			ReplacementType: "input_image",
		}, true, nil
	}
	if raw, matched := extractInlineFilePayload(block, blockType); matched {
		data, contentType, err := decodeInlinePayload(raw, contentTypeFromMap(block))
		if err != nil {
			return inlineDecodedFile{}, true, fmt.Errorf("invalid file input")
		}
		replacementType := "input_file"
		if isImageContentType(contentType) || looksLikeImageFilename(pickInlineFilename(block, contentType, "upload")) {
			replacementType = "input_image"
		}
		return inlineDecodedFile{
			Data:            data,
			ContentType:     contentType,
			Filename:        pickInlineFilename(block, contentType, defaultInlinePrefix(replacementType)),
			ReplacementType: replacementType,
		}, true, nil
	}
	return inlineDecodedFile{}, false, nil
}

// extractInlineImagePayload accepts OpenAI Chat / DeepSeek vision formats:
//   - {"type":"image_url","image_url":{"url":"data:..."|"https://..."}}
//   - compact {"type":"image","mediaType":"...","data":"..."}
//
// Responses input_image and Anthropic image+source blocks are intentionally
// unsupported. Returns either an inline payload string or a remote http(s) URL.
func extractInlineImagePayload(block map[string]any, blockType string) (payload string, remoteURL string, matched bool) {
	if isUnsupportedVisionBlockType(blockType) {
		return "", "", false
	}
	if blockType == "image_url" || blockType == "" {
		if payload, remoteURL, ok := extractInlineImageURL(block); ok {
			return payload, remoteURL, true
		}
	}
	if blockType != "image" {
		return "", "", false
	}
	for _, value := range []any{block["data"], block["base64"], block["image_data"], block["imageData"]} {
		if raw := strings.TrimSpace(shared.AsString(value)); raw != "" {
			return raw, "", true
		}
	}
	return "", "", false
}

func isUnsupportedVisionBlockType(blockType string) bool {
	blockType = strings.ToLower(strings.TrimSpace(blockType))
	return blockType == "input_image" || strings.HasPrefix(blockType, "input_image")
}

func extractInlineImageURL(block map[string]any) (payload string, remoteURL string, matched bool) {
	switch x := block["image_url"].(type) {
	case string:
		raw := strings.TrimSpace(x)
		if isDataURL(raw) {
			return raw, "", true
		}
		if isHTTPURL(raw) {
			return "", raw, true
		}
	case map[string]any:
		if raw := strings.TrimSpace(shared.AsString(x["url"])); raw != "" {
			if isDataURL(raw) {
				return raw, "", true
			}
			if isHTTPURL(raw) {
				return "", raw, true
			}
		}
		for _, value := range []any{x["data"], x["base64"]} {
			if raw := strings.TrimSpace(shared.AsString(value)); raw != "" {
				return raw, "", true
			}
		}
	}
	return "", "", false
}

func extractInlineFilePayload(block map[string]any, blockType string) (string, bool) {
	// Image-shaped blocks belong to extractInlineImagePayload.
	if strings.Contains(blockType, "image") {
		return "", false
	}
	for _, value := range []any{block["file_data"], block["base64"], block["data"]} {
		if raw := strings.TrimSpace(shared.AsString(value)); raw != "" {
			if strings.Contains(blockType, "file") || block["file_data"] != nil || block["filename"] != nil || block["file_name"] != nil || block["name"] != nil {
				return raw, true
			}
		}
	}
	return "", false
}

func fetchRemoteImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) > maxRemoteImageURLLen {
		return nil, "", fmt.Errorf("image url exceeds %d characters", maxRemoteImageURLLen)
	}
	if !isHTTPURL(rawURL) {
		return nil, "", fmt.Errorf("image url must be http(s)")
	}
	reqCtx := ctx
	cancel := func() {}
	if ctx == nil {
		reqCtx, cancel = context.WithTimeout(context.Background(), remoteImageFetchTimeout)
	} else if _, ok := ctx.Deadline(); !ok {
		reqCtx, cancel = context.WithTimeout(ctx, remoteImageFetchTimeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("invalid image url")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, int64(maxRemoteImageBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download image: %w", err)
	}
	if len(data) > maxRemoteImageBytes {
		return nil, "", fmt.Errorf("image exceeds %d MiB limit", maxRemoteImageBytes>>20)
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !isImageContentType(contentType) {
		// Still accept if magic bytes look like an image; otherwise reject.
		detected := http.DetectContentType(data)
		if !isImageContentType(detected) {
			return nil, "", fmt.Errorf("url did not return an image")
		}
		contentType = detected
	}
	return data, contentType, nil
}

func isHTTPURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

func isImageContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if comma := strings.Index(contentType, ";"); comma >= 0 {
		contentType = strings.TrimSpace(contentType[:comma])
	}
	switch contentType {
	case "image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return strings.HasPrefix(contentType, "image/")
	}
}

func looksLikeImageFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func decodeInlinePayload(raw string, explicitContentType string) ([]byte, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", fmt.Errorf("empty payload")
	}
	if isDataURL(raw) {
		return decodeDataURL(raw, explicitContentType)
	}
	decoded, err := decodeBase64Flexible(raw)
	if err != nil {
		return nil, "", err
	}
	contentType := strings.TrimSpace(explicitContentType)
	if contentType == "" && len(decoded) > 0 {
		contentType = http.DetectContentType(decoded)
	}
	return decoded, contentType, nil
}

func decodeDataURL(raw string, explicitContentType string) ([]byte, string, error) {
	raw = strings.TrimSpace(raw)
	if !isDataURL(raw) {
		return nil, "", fmt.Errorf("unsupported data url")
	}
	header, payload, ok := strings.Cut(raw, ",")
	if !ok {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := strings.TrimSpace(strings.TrimPrefix(header, "data:"))
	contentType := strings.TrimSpace(explicitContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
		if meta != "" {
			parts := strings.Split(meta, ";")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				contentType = strings.TrimSpace(parts[0])
			}
		}
	}
	if strings.Contains(strings.ToLower(meta), ";base64") {
		decoded, err := decodeBase64Flexible(payload)
		if err != nil {
			return nil, "", err
		}
		return decoded, contentType, nil
	}
	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return nil, "", err
	}
	return []byte(decoded), contentType, nil
}

func decodeBase64Flexible(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := enc.DecodeString(raw)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 payload")
}

func contentTypeFromMap(block map[string]any) string {
	for _, value := range []any{block["mime_type"], block["mimeType"], block["content_type"], block["contentType"], block["media_type"], block["mediaType"]} {
		if contentType := strings.TrimSpace(shared.AsString(value)); contentType != "" {
			return contentType
		}
	}
	if imageURL, ok := block["image_url"].(map[string]any); ok {
		for _, value := range []any{imageURL["mime_type"], imageURL["mimeType"], imageURL["content_type"], imageURL["contentType"]} {
			if contentType := strings.TrimSpace(shared.AsString(value)); contentType != "" {
				return contentType
			}
		}
	}
	return ""
}

func pickInlineFilename(block map[string]any, contentType string, prefix string) string {
	for _, value := range []any{block["filename"], block["file_name"], block["name"]} {
		if name := strings.TrimSpace(shared.AsString(value)); name != "" {
			return filepath.Base(name)
		}
	}
	if prefix == "" {
		prefix = "upload"
	}
	ext := ".bin"
	if parsedType := strings.TrimSpace(contentType); parsedType != "" {
		if comma := strings.Index(parsedType, ";"); comma >= 0 {
			parsedType = strings.TrimSpace(parsedType[:comma])
		}
		if exts, err := mime.ExtensionsByType(parsedType); err == nil && len(exts) > 0 && strings.TrimSpace(exts[0]) != "" {
			ext = exts[0]
		}
	}
	return prefix + ext
}

func defaultInlinePrefix(blockType string) string {
	blockType = strings.ToLower(strings.TrimSpace(blockType))
	if strings.Contains(blockType, "image") {
		return "image"
	}
	return "upload"
}

func isDataURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:")
}

func stringsToAnySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
