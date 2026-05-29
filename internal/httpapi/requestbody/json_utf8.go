package requestbody

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidUTF8Body     = errors.New("invalid utf-8 request body")
	errRequestBodyTooLarge = errors.New("request body too large")
)

const maxJSONUTF8ValidationSize = 100 << 20

// ValidateJSONUTF8 validates complete JSON request bodies before downstream
// handlers run (including auth), so malformed UTF-8 is rejected without
// triggering managed-account login or upstream calls.
func ValidateJSONUTF8(next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldValidateJSONBody(r) {
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, maxJSONUTF8ValidationSize+1))
			_ = r.Body.Close()
			if readErr != nil {
				writeJSONUTF8ValidationError(w, http.StatusBadRequest, `{"error":{"message":"invalid json","type":"invalid_request_error"}}`)
				return
			}
			if len(raw) > maxJSONUTF8ValidationSize {
				writeJSONUTF8ValidationError(w, http.StatusRequestEntityTooLarge, `{"error":{"message":"request body too large","type":"invalid_request_error"}}`)
				return
			}
			if !utf8.Valid(raw) {
				writeJSONUTF8ValidationError(w, http.StatusBadRequest, `{"error":{"message":"invalid json (invalid utf-8 body)","type":"invalid_request_error"}}`)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSONUTF8ValidationError(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func shouldValidateJSONBody(r *http.Request) bool {
	if r == nil || r.Body == nil {
		return false
	}
	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}
	return isJSONContentType(r.Header.Get("Content-Type")) || isKnownJSONRequestPath(r.Method, path)
}

func isJSONContentType(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		mediaType = raw
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return strings.Contains(mediaType, "json")
}

func isKnownJSONRequestPath(method, path string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch {
	case path == "/v1/chat/completions" || path == "/chat/completions":
		return true
	case path == "/v1/responses" || path == "/responses":
		return true
	case path == "/v1/embeddings" || path == "/embeddings":
		return true
	case path == "/anthropic/v1/messages" || path == "/v1/messages" || path == "/messages":
		return true
	case path == "/anthropic/v1/messages/count_tokens" || path == "/v1/messages/count_tokens" || path == "/messages/count_tokens":
		return true
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		return strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")
	case strings.HasPrefix(path, "/admin/"):
		return true
	default:
		return false
	}
}
