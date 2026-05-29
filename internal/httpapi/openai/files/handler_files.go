package files

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/shared"
)

const openAIUploadMaxMemory = 32 << 20

type Handler struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	DS          shared.DeepSeekCaller
	ChatHistory *chathistory.Store
}

type fileFetcher interface {
	FetchUploadedFile(ctx context.Context, a *auth.RequestAuth, fileID string) (*dsclient.UploadFileResult, error)
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data") {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "content-type must be multipart/form-data")
		return
	}
	// Enforce a hard cap on the total request body size to prevent OOM
	r.Body = http.MaxBytesReader(w, r.Body, shared.UploadMaxSize)
	if err := r.ParseMultipartForm(openAIUploadMaxMemory); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			shared.WriteOpenAIError(w, http.StatusRequestEntityTooLarge, "file size exceeds limit")
			return
		}
		shared.WriteOpenAIError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	file, header, err := r.FormFile("file")
	if err != nil {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" && len(data) > 0 {
		contentType = http.DetectContentType(data)
	}
	modelType := resolveUploadModelType(h.Store, r)
	result, err := h.DS.UploadFile(r.Context(), a, dsclient.UploadFileRequest{
		Filename:    header.Filename,
		ContentType: contentType,
		Purpose:     strings.TrimSpace(r.FormValue("purpose")),
		ModelType:   modelType,
		Data:        data,
	}, 3)
	if err != nil {
		shared.WriteOpenAIError(w, http.StatusInternalServerError, "Failed to upload file.")
		return
	}
	if result != nil && result.AccountID == "" {
		result.AccountID = a.AccountID
	}
	shared.WriteJSON(w, http.StatusOK, buildOpenAIFileObject(result))
}

func (h *Handler) RetrieveFile(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	fileID := strings.TrimSpace(chi.URLParam(r, "file_id"))
	if fileID == "" {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "file_id is required")
		return
	}
	fetcher, ok := h.DS.(fileFetcher)
	if !ok {
		shared.WriteOpenAIError(w, http.StatusNotImplemented, "file retrieval is not available")
		return
	}
	result, err := fetcher.FetchUploadedFile(r.Context(), a, fileID)
	if err != nil {
		if errors.Is(err, dsclient.ErrUploadFileNotFound) {
			shared.WriteOpenAIError(w, http.StatusNotFound, "file not found")
			return
		}
		shared.WriteOpenAIError(w, http.StatusInternalServerError, "Failed to retrieve file.")
		return
	}
	if result != nil && result.AccountID == "" {
		result.AccountID = a.AccountID
	}
	shared.WriteJSON(w, http.StatusOK, buildOpenAIFileObject(result))
}

func resolveUploadModelType(store shared.ConfigReader, r *http.Request) string {
	for _, candidate := range []string{r.FormValue("model_type"), r.Header.Get("X-Model-Type")} {
		if modelType := normalizeUploadModelType(candidate); modelType != "" {
			return config.UpstreamSafeModelType(modelType)
		}
	}
	requestedModel := strings.TrimSpace(r.FormValue("model"))
	if requestedModel != "" {
		if resolvedModel, ok := config.ResolveModel(requestedModel); ok {
			if modelType, ok := config.GetModelType(config.UpstreamDeepSeekSKU(resolvedModel)); ok {
				return config.UpstreamSafeModelType(modelType)
			}
		}
	}
	return config.UpstreamSafeModelType("default")
}

func normalizeUploadModelType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "vision":
		return "vision"
	case "default", "expert":
		return "default"
	default:
		return ""
	}
}

func buildOpenAIFileObject(result *dsclient.UploadFileResult) map[string]any {
	if result == nil {
		obj := map[string]any{
			"id":             "",
			"object":         "file",
			"bytes":          0,
			"created_at":     time.Now().Unix(),
			"filename":       "",
			"purpose":        "",
			"status":         "uploaded",
			"status_details": nil,
		}
		return obj
	}
	obj := map[string]any{
		"id":             result.ID,
		"object":         "file",
		"bytes":          result.Bytes,
		"created_at":     time.Now().Unix(),
		"filename":       result.Filename,
		"purpose":        result.Purpose,
		"status":         result.Status,
		"status_details": nil,
	}
	if result.AccountID != "" {
		obj["account_id"] = result.AccountID
	}
	return obj
}
