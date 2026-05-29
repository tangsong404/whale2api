package completionruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"whale2api/internal/assistantturn"
	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/privatecontext"
	"whale2api/internal/promptcompat"
	"whale2api/internal/sse"
)

type DeepSeekCaller interface {
	CreateSession(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	GetPow(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	UploadFile(ctx context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int) (*dsclient.UploadFileResult, error)
	CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error)
}

type Options struct {
	StripReferenceMarkers bool
	MaxAttempts           int
	RetryEnabled          bool
	RetryMaxAttempts      int
	// ClientHTTPRequestContentLength, when non-nil, is included in upstream_empty_output logs (see net/http: -1 if unknown).
	ClientHTTPRequestContentLength *int64
}

type NonStreamResult struct {
	SessionID string
	Payload   map[string]any
	Turn      assistantturn.Turn
	Attempts  int
}

type StartResult struct {
	SessionID string
	Payload   map[string]any
	Pow       string
	Response  *http.Response
	Request   promptcompat.StandardRequest
}

func StartCompletion(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (StartResult, *assistantturn.OutputError) {
	if gateErr := userContextOverGate(stdReq); gateErr != nil {
		return StartResult{Request: stdReq}, gateErr
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	sessionID, err := ds.CreateSession(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{Request: stdReq}, authOutputError(a)
	}
	pow, err := ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{SessionID: sessionID, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	var uploadErr error
	stdReq, uploadErr = ensurePrivateContextForCurrentAccount(ctx, ds, a, stdReq, maxAttempts)
	if uploadErr != nil {
		return StartResult{SessionID: sessionID, Pow: pow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to prepare request.", Code: "error"}
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := ds.CallCompletion(ctx, a, payload, pow, maxAttempts)
	if err != nil {
		return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
	}
	return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Response: resp, Request: stdReq}, nil
}

func ensurePrivateContextForCurrentAccount(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, maxAttempts int) (promptcompat.StandardRequest, error) {
	contextText := strings.TrimSpace(stdReq.PrivateContextText)
	if contextText == "" || ds == nil || a == nil {
		return stdReq, nil
	}
	accountID := strings.TrimSpace(a.AccountID)
	if accountID != "" && strings.EqualFold(accountID, strings.TrimSpace(stdReq.PrivateContextAccountID)) && strings.TrimSpace(stdReq.PrivateContextRefFileID) != "" {
		return stdReq, nil
	}
	modelType := "default"
	if resolvedType, ok := config.GetModelType(config.UpstreamDeepSeekSKU(stdReq.ResolvedModel)); ok {
		modelType = resolvedType
	}
	uploadReq := dsclient.UploadFileRequest{
		Filename:    privateContextFilename(),
		ContentType: "text/plain; charset=utf-8",
		Purpose:     "assistants",
		ModelType:   modelType,
		Data:        []byte(stdReq.PrivateContextText),
	}
	result, err := privatecontext.Upload(ctx, ds, a, uploadReq, maxAttempts, privatecontext.Key(a, modelType, stdReq.PrivateContextText))
	if err != nil {
		return stdReq, err
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return stdReq, fmt.Errorf("empty private context file id")
	}
	stdReq.RefFileIDs = replacePrivateContextRefFileID(stdReq.RefFileIDs, stdReq.PrivateContextRefFileID, fileID)
	stdReq.PrivateContextAccountID = accountID
	stdReq.PrivateContextRefFileID = fileID
	return stdReq, nil
}

func privateContextFilename() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:]) + ".txt"
	}
	return "000000000000000000000000.txt"
}

func replacePrivateContextRefFileID(existing []string, oldID string, newID string) []string {
	newID = strings.TrimSpace(newID)
	if newID == "" {
		return existing
	}
	oldID = strings.TrimSpace(oldID)
	out := make([]string, 0, len(existing)+1)
	seenNew := false
	for _, id := range existing {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || (oldID != "" && strings.EqualFold(trimmed, oldID)) {
			continue
		}
		if strings.EqualFold(trimmed, newID) {
			seenNew = true
		}
		out = append(out, trimmed)
	}
	if !seenNew {
		out = append([]string{newID}, out...)
	}
	return out
}

func ExecuteNonStreamWithRetry(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (NonStreamResult, *assistantturn.OutputError) {
	start, startErr := StartCompletion(ctx, ds, a, stdReq, opts)
	if startErr != nil {
		return NonStreamResult{SessionID: start.SessionID, Payload: start.Payload}, startErr
	}
	stdReq = start.Request
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	sessionID := start.SessionID
	payload := start.Payload
	pow := start.Pow

	attempts := 0
	currentResp := start.Response
	usagePrompt := stdReq.PromptTokenText
	accumulatedThinking := ""
	accumulatedRawThinking := ""
	accumulatedToolDetectionThinking := ""
	for {
		turn, outErr := collectAttempt(ctx, a, currentResp, stdReq, usagePrompt, opts)
		if outErr != nil {
			return NonStreamResult{SessionID: sessionID, Payload: payload, Attempts: attempts}, outErr
		}
		accumulatedThinking += sse.TrimContinuationOverlap(accumulatedThinking, turn.Thinking)
		accumulatedRawThinking += sse.TrimContinuationOverlap(accumulatedRawThinking, turn.RawThinking)
		accumulatedToolDetectionThinking += sse.TrimContinuationOverlap(accumulatedToolDetectionThinking, turn.DetectionThinking)
		turn.Thinking = accumulatedThinking
		turn.RawThinking = accumulatedRawThinking
		turn.DetectionThinking = accumulatedToolDetectionThinking
		turn = assistantturn.BuildTurnFromCollected(sse.CollectResult{
			Text:                  turn.RawText,
			Thinking:              turn.RawThinking,
			ToolDetectionThinking: turn.DetectionThinking,
			ContentFilter:         turn.ContentFilter,
			CitationLinks:         turn.CitationLinks,
			ResponseMessageID:     turn.ResponseMessageID,
		}, buildOptions(stdReq, usagePrompt, opts))

		retryMax := opts.RetryMaxAttempts
		if retryMax <= 0 {
			retryMax = shared.EmptyOutputRetryMaxAttempts()
		}
		if !opts.RetryEnabled || !assistantturn.ShouldRetryEmptyOutput(turn, attempts, retryMax) {
			if turn.Error != nil && turn.Error.Code == "upstream_empty_output" {
				assistantturn.LogUpstreamEmptyOutputDiagnostic(stdReq.Surface, false, "nonstream_terminal", turn, payload, opts.ClientHTTPRequestContentLength)
			}
			return NonStreamResult{SessionID: sessionID, Payload: payload, Turn: turn, Attempts: attempts}, turn.Error
		}

		attempts++
		config.Logger.Info("[completion_runtime_empty_retry] attempting synthetic retry", "surface", stdReq.Surface, "stream", false, "retry_attempt", attempts, "parent_message_id", turn.ResponseMessageID)
		retryPow, powErr := ds.GetPow(ctx, a, maxAttempts)
		if powErr != nil {
			config.Logger.Warn("[completion_runtime_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", stdReq.Surface, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		retryPayload := shared.ClonePayloadForEmptyOutputRetry(payload, turn.ResponseMessageID)
		nextResp, err := ds.CallCompletion(ctx, a, retryPayload, retryPow, maxAttempts)
		if err != nil {
			return NonStreamResult{SessionID: sessionID, Payload: payload, Turn: turn, Attempts: attempts}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
		}
		usagePrompt = shared.UsagePromptWithEmptyOutputRetry(usagePrompt, attempts)
		currentResp = nextResp
	}
}

func collectAttempt(ctx context.Context, a *auth.RequestAuth, resp *http.Response, stdReq promptcompat.StandardRequest, usagePrompt string, opts Options) (assistantturn.Turn, *assistantturn.OutputError) {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			config.Logger.Warn("[completion_runtime] response body close failed", "surface", stdReq.Surface, "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if a != nil {
			a.TryAutoDiscardHTTPBody(ctx, body)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return assistantturn.Turn{}, &assistantturn.OutputError{Status: resp.StatusCode, Message: message, Code: "error"}
	}
	result := sse.CollectStream(resp, stdReq.Thinking, false)
	return assistantturn.BuildTurnFromCollected(result, buildOptions(stdReq, usagePrompt, opts)), nil
}

func buildOptions(stdReq promptcompat.StandardRequest, prompt string, opts Options) assistantturn.BuildOptions {
	return assistantturn.BuildOptions{
		Model:                 stdReq.ResponseModel,
		Prompt:                prompt,
		RefFileTokens:         stdReq.RefFileTokens,
		SearchEnabled:         stdReq.Search,
		StripReferenceMarkers: opts.StripReferenceMarkers,
		ToolNames:             stdReq.ToolNames,
		ToolsRaw:              stdReq.ToolsRaw,
		ToolChoice:            stdReq.ToolChoice,
	}
}

func authOutputError(a *auth.RequestAuth) *assistantturn.OutputError {
	if a != nil && a.UseConfigToken {
		return &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Account token is invalid. Please re-login the account in pool UI.", Code: "error"}
	}
	return &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Invalid token. If this should be a Whale2API key, add it to config.keys first.", Code: "error"}
}

