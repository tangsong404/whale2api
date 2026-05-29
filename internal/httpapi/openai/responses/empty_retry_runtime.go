package responses

import (
	"io"
	"net/http"
	"strings"
	"time"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsprotocol "whale2api/internal/deepseek/protocol"
	"whale2api/internal/promptcompat"
	"whale2api/internal/responsehistory"
	streamengine "whale2api/internal/stream"
)

func (h *Handler) handleResponsesStreamWithRetry(w http.ResponseWriter, r *http.Request, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string, historySession *responsehistory.Session, streamReq promptcompat.StandardRequest) {
	streamRuntime, initialType, ok := h.prepareResponsesStreamRuntime(w, resp, owner, responseID, model, finalPrompt, refFileTokens, thinkingEnabled, searchEnabled, toolNames, toolsRaw, toolChoice, traceID, historySession)
	if !ok {
		return
	}
	streamRuntime.logSurface = strings.TrimSpace(streamReq.Surface)
	cl := r.ContentLength
	clPtr := &cl
	streamRuntime.attachUpstreamEmptyDiagnostics(payload, clPtr)
	currentResp := resp
	attempts := 0
	for {
		streamRuntime.attachUpstreamEmptyDiagnostics(payload, clPtr)
		terminalWritten, retryable := h.consumeResponsesStreamAttempt(r, currentResp, streamRuntime, initialType, thinkingEnabled, attempts < emptyOutputRetryMaxAttempts())
		if terminalWritten {
			logResponsesStreamTerminal(streamRuntime, attempts)
			return
		}
		if !retryable || !emptyOutputRetryEnabled() || attempts >= emptyOutputRetryMaxAttempts() {
			streamRuntime.finalize("stop", false)
			config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
			return
		}
		attempts++
		config.Logger.Info("[openai_empty_retry] attempting synthetic retry", "surface", "responses", "stream", true, "retry_attempt", attempts, "parent_message_id", streamRuntime.responseMessageID)
		retryPow, powErr := h.DS.GetPow(r.Context(), a, 3)
		if powErr != nil {
			config.Logger.Warn("[openai_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", "responses", "stream", true, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		nextResp, err := h.DS.CallCompletion(r.Context(), a, clonePayloadForEmptyOutputRetry(payload, streamRuntime.responseMessageID), retryPow, 3)
		if err != nil {
			streamRuntime.failResponse(http.StatusInternalServerError, "Failed to get completion.", "error")
			config.Logger.Warn("[openai_empty_retry] retry request failed", "surface", "responses", "stream", true, "retry_attempt", attempts, "error", err)
			return
		}
		if nextResp.StatusCode != http.StatusOK {
			defer func() { _ = nextResp.Body.Close() }()
			body, _ := io.ReadAll(nextResp.Body)
			streamRuntime.failResponse(nextResp.StatusCode, strings.TrimSpace(string(body)), "error")
			return
		}
		streamRuntime.finalPrompt = usagePromptWithEmptyOutputRetry(finalPrompt, attempts)
		currentResp = nextResp
	}
}

func (h *Handler) prepareResponsesStreamRuntime(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string, historySession *responsehistory.Session) (*responsesStreamRuntime, string, bool) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if historySession != nil {
			historySession.Error(resp.StatusCode, strings.TrimSpace(string(body)), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return nil, "", false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamRuntime := newResponsesStreamRuntime(
		w, rc, canFlush, responseID, model, finalPrompt, thinkingEnabled, searchEnabled,
		stripReferenceMarkersEnabled(), toolNames, toolsRaw, len(toolNames) > 0,
		h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence(),
		toolChoice, traceID, func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		}, historySession,
	)
	streamRuntime.refFileTokens = refFileTokens
	streamRuntime.sendCreated()
	return streamRuntime, initialType, true
}

func (h *Handler) consumeResponsesStreamAttempt(r *http.Request, resp *http.Response, streamRuntime *responsesStreamRuntime, initialType string, thinkingEnabled bool, allowDeferEmpty bool) (bool, bool) {
	defer func() { _ = resp.Body.Close() }()
	finalReason := "stop"
	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(dsprotocol.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(dsprotocol.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: dsprotocol.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if string(reason) == "content_filter" {
				finalReason = "content_filter"
			}
		},
		OnContextDone: func() {
			streamRuntime.markContextCancelled()
		},
	})
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		return true, false
	}
	terminalWritten := streamRuntime.finalize(finalReason, allowDeferEmpty && finalReason != "content_filter")
	if terminalWritten {
		return true, false
	}
	return false, true
}

func logResponsesStreamTerminal(streamRuntime *responsesStreamRuntime, attempts int) {
	source := "first_attempt"
	if attempts > 0 {
		source = "synthetic_retry"
	}
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		config.Logger.Info("[openai_empty_retry] terminal cancelled", "surface", "responses", "stream", true, "retry_attempts", attempts, "error_code", streamRuntime.finalErrorCode)
		return
	}
	if streamRuntime.failed {
		config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
		return
	}
	config.Logger.Info("[openai_empty_retry] completed", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", source)
}
