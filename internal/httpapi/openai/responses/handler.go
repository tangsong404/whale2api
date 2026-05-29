package responses

import (
	"context"
	"net/http"
	"sync"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/httpapi/openai/files"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/textclean"
	"whale2api/internal/toolstream"
)

const openAIGeneralMaxSize = shared.GeneralMaxSize

var writeJSON = shared.WriteJSON

type Handler struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	DS          shared.DeepSeekCaller
	ChatHistory *chathistory.Store

	responsesMu sync.Mutex
	responses   *responseStore
}

func stripReferenceMarkersEnabled() bool {
	return textclean.StripReferenceMarkersEnabled()
}

func (h *Handler) preprocessInlineFileInputs(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	if h == nil {
		return nil
	}
	return (&files.Handler{Store: h.Store, Auth: h.Auth, DS: h.DS, ChatHistory: h.ChatHistory}).PreprocessInlineFileInputs(ctx, a, req)
}

func (h *Handler) toolcallFeatureMatchEnabled() bool {
	if h == nil {
		return shared.ToolcallFeatureMatchEnabled(nil)
	}
	return shared.ToolcallFeatureMatchEnabled(h.Store)
}

func (h *Handler) toolcallEarlyEmitHighConfidence() bool {
	if h == nil {
		return shared.ToolcallEarlyEmitHighConfidence(nil)
	}
	return shared.ToolcallEarlyEmitHighConfidence(h.Store)
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	shared.WriteOpenAIError(w, status, message)
}

func writeOpenAIErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	shared.WriteOpenAIErrorWithCode(w, status, message, code)
}

func writeOpenAIErrorWithCodeAndParam(w http.ResponseWriter, status int, message, code, param string) {
	shared.WriteOpenAIErrorWithCodeAndParam(w, status, message, code, param)
}

func openAIErrorType(status int) string {
	return shared.OpenAIErrorType(status)
}

func writeOpenAIInlineFileError(w http.ResponseWriter, err error) {
	files.WriteInlineFileError(w, err)
}

func requestTraceID(r *http.Request) string {
	return shared.RequestTraceID(r)
}

func cleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	return shared.CleanVisibleOutput(text, stripReferenceMarkers)
}

func emptyOutputRetryEnabled() bool {
	return shared.EmptyOutputRetryEnabled()
}

func emptyOutputRetryMaxAttempts() int {
	return shared.EmptyOutputRetryMaxAttempts()
}

func clonePayloadForEmptyOutputRetry(payload map[string]any, parentMessageID int) map[string]any {
	return shared.ClonePayloadForEmptyOutputRetry(payload, parentMessageID)
}

func usagePromptWithEmptyOutputRetry(originalPrompt string, retryAttempts int) string {
	return shared.UsagePromptWithEmptyOutputRetry(originalPrompt, retryAttempts)
}

func filterIncrementalToolCallDeltasByAllowed(deltas []toolstream.ToolCallDelta, seenNames map[int]string) []toolstream.ToolCallDelta {
	return shared.FilterIncrementalToolCallDeltasByAllowed(deltas, seenNames)
}
