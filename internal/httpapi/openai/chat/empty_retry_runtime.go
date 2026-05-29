package chat

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"whale2api/internal/assistantturn"
	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsprotocol "whale2api/internal/deepseek/protocol"
	openaifmt "whale2api/internal/format/openai"
	"whale2api/internal/promptcompat"
	"whale2api/internal/sse"
	streamengine "whale2api/internal/stream"
)

type chatNonStreamResult struct {
	rawThinking           string
	rawText               string
	thinking              string
	toolDetectionThinking string
	text                  string
	contentFilter         bool
	detectedCalls         int
	body                  map[string]any
	finishReason          string
	responseMessageID     int
	outputError           *assistantturn.OutputError
}

func (r chatNonStreamResult) historyText() string {
	return historyTextForArchive(r.rawText, r.text)
}

func (r chatNonStreamResult) historyThinking() string {
	return historyThinkingForArchive(r.rawThinking, r.toolDetectionThinking, r.thinking)
}

func (h *Handler) handleNonStreamWithRetry(w http.ResponseWriter, ctx context.Context, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, completionID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, historySession *chatHistorySession, clientContentLength *int64, logSurface string) {
	attempts := 0
	currentResp := resp
	usagePrompt := finalPrompt
	accumulatedThinking := ""
	accumulatedRawThinking := ""
	accumulatedToolDetectionThinking := ""
	for {
		result, ok := h.collectChatNonStreamAttempt(ctx, a, w, currentResp, completionID, model, usagePrompt, thinkingEnabled, searchEnabled, toolNames, toolsRaw)
		if !ok {
			return
		}
		accumulatedThinking += sse.TrimContinuationOverlap(accumulatedThinking, result.thinking)
		accumulatedRawThinking += sse.TrimContinuationOverlap(accumulatedRawThinking, result.rawThinking)
		accumulatedToolDetectionThinking += sse.TrimContinuationOverlap(accumulatedToolDetectionThinking, result.toolDetectionThinking)
		result.thinking = accumulatedThinking
		result.rawThinking = accumulatedRawThinking
		result.toolDetectionThinking = accumulatedToolDetectionThinking
		detected := detectAssistantToolCalls(result.rawText, result.text, result.rawThinking, result.toolDetectionThinking, toolNames)
		result.detectedCalls = len(detected.Calls)
		result.body = openaifmt.BuildChatCompletionWithToolCalls(completionID, model, usagePrompt, result.thinking, result.text, detected.Calls, toolsRaw)
		addRefFileTokensToUsage(result.body, refFileTokens)
		result.finishReason = chatFinishReason(result.body)
		if !shouldRetryChatNonStream(result, attempts) {
			h.finishChatNonStreamResult(w, result, attempts, model, payload, usagePrompt, refFileTokens, historySession, clientContentLength, logSurface)
			return
		}

		attempts++
		config.Logger.Info("[openai_empty_retry] attempting synthetic retry", "surface", "chat.completions", "stream", false, "retry_attempt", attempts, "parent_message_id", result.responseMessageID)
		retryPow, powErr := h.DS.GetPow(ctx, a, 3)
		if powErr != nil {
			config.Logger.Warn("[openai_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", "chat.completions", "stream", false, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		retryPayload := clonePayloadForEmptyOutputRetry(payload, result.responseMessageID)
		nextResp, err := h.DS.CallCompletion(ctx, a, retryPayload, retryPow, 3)
		if err != nil {
			if historySession != nil {
				historySession.error(http.StatusInternalServerError, "Failed to get completion.", "error", result.historyThinking(), result.historyText())
			}
			writeOpenAIError(w, http.StatusInternalServerError, "Failed to get completion.")
			config.Logger.Warn("[openai_empty_retry] retry request failed", "surface", "chat.completions", "stream", false, "retry_attempt", attempts, "error", err)
			return
		}
		usagePrompt = usagePromptWithEmptyOutputRetry(usagePrompt, attempts)
		currentResp = nextResp
	}
}

func (h *Handler) collectChatNonStreamAttempt(ctx context.Context, a *auth.RequestAuth, w http.ResponseWriter, resp *http.Response, completionID, model, usagePrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any) (chatNonStreamResult, bool) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if a != nil {
			a.TryAutoDiscardHTTPBody(ctx, body)
		}
		writeOpenAIError(w, resp.StatusCode, string(body))
		return chatNonStreamResult{}, false
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)
	turn := assistantturn.BuildTurnFromCollected(result, assistantturn.BuildOptions{
		Model:         model,
		Prompt:        usagePrompt,
		SearchEnabled: searchEnabled,
		ToolNames:     toolNames,
		ToolsRaw:      toolsRaw,
	})
	respBody := openaifmt.BuildChatCompletionWithToolCalls(completionID, model, usagePrompt, turn.Thinking, turn.Text, turn.ToolCalls, toolsRaw)
	return chatNonStreamResult{
		rawThinking:           result.Thinking,
		rawText:               result.Text,
		thinking:              turn.Thinking,
		toolDetectionThinking: result.ToolDetectionThinking,
		text:                  turn.Text,
		contentFilter:         result.ContentFilter,
		detectedCalls:         len(turn.ToolCalls),
		body:                  respBody,
		finishReason:          chatFinishReason(respBody),
		responseMessageID:     result.ResponseMessageID,
		outputError:           turn.Error,
	}, true
}

func (h *Handler) finishChatNonStreamResult(w http.ResponseWriter, result chatNonStreamResult, attempts int, model string, payload map[string]any, usagePrompt string, refFileTokens int, historySession *chatHistorySession, clientContentLength *int64, logSurface string) {
	if result.detectedCalls == 0 && strings.TrimSpace(result.text) == "" {
		status, message, code := upstreamEmptyOutputDetail(result.contentFilter, result.text, result.thinking)
		if result.outputError != nil {
			status, message, code = result.outputError.Status, result.outputError.Message, result.outputError.Code
		}
		if code == "upstream_empty_output" {
			surf := strings.TrimSpace(logSurface)
			if surf == "" {
				surf = "openai_chat"
			}
			assistantturn.LogUpstreamEmptyOutputDiagnostic(surf, false, "chat_nonstream_terminal", assistantturn.Turn{
				Model:             model,
				Prompt:            usagePrompt,
				RawThinking:       result.rawThinking,
				Thinking:          result.thinking,
				Text:              result.text,
				DetectionThinking: result.toolDetectionThinking,
				ContentFilter:     result.contentFilter,
				ResponseMessageID: result.responseMessageID,
				Error:             &assistantturn.OutputError{Status: status, Message: message, Code: code},
			}, payload, clientContentLength)
		}
		if historySession != nil {
			historySession.error(status, message, code, result.historyThinking(), result.historyText())
		}
		writeOpenAIErrorWithCode(w, status, message, code)
		config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "chat.completions", "stream", false, "retry_attempts", attempts, "success_source", "none", "content_filter", result.contentFilter)
		return
	}
	if historySession != nil {
		historySession.success(http.StatusOK, result.historyThinking(), result.historyText(), result.finishReason, openaifmt.BuildChatUsageForModel("", usagePrompt, result.thinking, result.text, refFileTokens))
	}
	writeJSON(w, http.StatusOK, result.body)
	source := "first_attempt"
	if attempts > 0 {
		source = "synthetic_retry"
	}
	config.Logger.Info("[openai_empty_retry] completed", "surface", "chat.completions", "stream", false, "retry_attempts", attempts, "success_source", source)
}

func chatFinishReason(respBody map[string]any) string {
	if choices, ok := respBody["choices"].([]map[string]any); ok && len(choices) > 0 {
		if fr, _ := choices[0]["finish_reason"].(string); strings.TrimSpace(fr) != "" {
			return fr
		}
	}
	return "stop"
}

func shouldRetryChatNonStream(result chatNonStreamResult, attempts int) bool {
	return emptyOutputRetryEnabled() &&
		attempts < emptyOutputRetryMaxAttempts() &&
		!result.contentFilter &&
		result.detectedCalls == 0 &&
		strings.TrimSpace(result.text) == ""
}

func (h *Handler) handleStreamWithRetry(w http.ResponseWriter, r *http.Request, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, completionID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, streamReq promptcompat.StandardRequest, historySession *chatHistorySession) {
	streamRuntime, initialType, ok := h.prepareChatStreamRuntime(w, r.Context(), a, resp, completionID, model, finalPrompt, refFileTokens, thinkingEnabled, searchEnabled, toolNames, toolsRaw, toolChoice, historySession)
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
		terminalWritten, retryable := h.consumeChatStreamAttempt(r, currentResp, streamRuntime, initialType, thinkingEnabled, historySession, attempts < emptyOutputRetryMaxAttempts())
		if terminalWritten {
			logChatStreamTerminal(streamRuntime, attempts)
			return
		}
		if !retryable || !emptyOutputRetryEnabled() || attempts >= emptyOutputRetryMaxAttempts() {
			streamRuntime.finalize("stop", false)
			recordChatStreamHistory(streamRuntime, historySession)
			config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", "none")
			return
		}
		attempts++
		config.Logger.Info("[openai_empty_retry] attempting synthetic retry", "surface", "chat.completions", "stream", true, "retry_attempt", attempts, "parent_message_id", streamRuntime.responseMessageID)
		retryPow, powErr := h.DS.GetPow(r.Context(), a, 3)
		if powErr != nil {
			config.Logger.Warn("[openai_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", "chat.completions", "stream", true, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		nextResp, err := h.DS.CallCompletion(r.Context(), a, clonePayloadForEmptyOutputRetry(payload, streamRuntime.responseMessageID), retryPow, 3)
		if err != nil {
			failChatStreamRetry(streamRuntime, historySession, http.StatusInternalServerError, "Failed to get completion.", "error")
			config.Logger.Warn("[openai_empty_retry] retry request failed", "surface", "chat.completions", "stream", true, "retry_attempt", attempts, "error", err)
			return
		}
		if nextResp.StatusCode != http.StatusOK {
			defer func() { _ = nextResp.Body.Close() }()
			body, _ := io.ReadAll(nextResp.Body)
			if a != nil {
				a.TryAutoDiscardHTTPBody(r.Context(), body)
			}
			failChatStreamRetry(streamRuntime, historySession, nextResp.StatusCode, string(body), "error")
			return
		}
		streamRuntime.finalPrompt = usagePromptWithEmptyOutputRetry(finalPrompt, attempts)
		currentResp = nextResp
	}
}

func (h *Handler) prepareChatStreamRuntime(w http.ResponseWriter, ctx context.Context, a *auth.RequestAuth, resp *http.Response, completionID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, historySession *chatHistorySession) (*chatStreamRuntime, string, bool) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if a != nil {
			a.TryAutoDiscardHTTPBody(ctx, body)
		}
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, string(body))
		return nil, "", false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if !canFlush {
		config.Logger.Warn("[stream] response writer does not support flush; streaming may be buffered")
	}
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamRuntime := newChatStreamRuntime(
		w, rc, canFlush, completionID, time.Now().Unix(), model, finalPrompt,
		thinkingEnabled, searchEnabled, stripReferenceMarkersEnabled(), toolNames, toolsRaw,
		toolChoice,
		len(toolNames) > 0, h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence(),
	)
	streamRuntime.refFileTokens = refFileTokens
	return streamRuntime, initialType, true
}

func (h *Handler) consumeChatStreamAttempt(r *http.Request, resp *http.Response, streamRuntime *chatStreamRuntime, initialType string, thinkingEnabled bool, historySession *chatHistorySession, allowDeferEmpty bool) (bool, bool) {
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
		OnKeepAlive: streamRuntime.sendKeepAlive,
		OnParsed: func(parsed sse.LineResult) streamengine.ParsedDecision {
			decision := streamRuntime.onParsed(parsed)
			if historySession != nil {
				historySession.progress(streamRuntime.historyThinking(), streamRuntime.historyText())
			}
			return decision
		},
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if string(reason) == "content_filter" {
				finalReason = "content_filter"
			}
		},
		OnContextDone: func() {
			streamRuntime.markContextCancelled()
			if historySession != nil {
				historySession.stopped(streamRuntime.historyThinking(), streamRuntime.historyText(), string(streamengine.StopReasonContextCancelled))
			}
		},
	})
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		return true, false
	}
	terminalWritten := streamRuntime.finalize(finalReason, allowDeferEmpty && finalReason != "content_filter")
	if terminalWritten {
		recordChatStreamHistory(streamRuntime, historySession)
		return true, false
	}
	return false, true
}

func recordChatStreamHistory(streamRuntime *chatStreamRuntime, historySession *chatHistorySession) {
	if historySession == nil {
		return
	}
	if streamRuntime.finalErrorMessage != "" {
		historySession.error(streamRuntime.finalErrorStatus, streamRuntime.finalErrorMessage, streamRuntime.finalErrorCode, streamRuntime.historyThinking(), streamRuntime.historyText())
		return
	}
	historySession.success(http.StatusOK, streamRuntime.historyThinking(), streamRuntime.historyText(), streamRuntime.finalFinishReason, streamRuntime.finalUsage)
}

func failChatStreamRetry(streamRuntime *chatStreamRuntime, historySession *chatHistorySession, status int, message, code string) {
	streamRuntime.sendFailedChunk(status, message, code)
	if historySession != nil {
		historySession.error(status, message, code, streamRuntime.historyThinking(), streamRuntime.historyText())
	}
}

func logChatStreamTerminal(streamRuntime *chatStreamRuntime, attempts int) {
	source := "first_attempt"
	if attempts > 0 {
		source = "synthetic_retry"
	}
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		config.Logger.Info("[openai_empty_retry] terminal cancelled", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "error_code", streamRuntime.finalErrorCode)
		return
	}
	if streamRuntime.finalErrorMessage != "" {
		config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
		return
	}
	config.Logger.Info("[openai_empty_retry] completed", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", source)
}
