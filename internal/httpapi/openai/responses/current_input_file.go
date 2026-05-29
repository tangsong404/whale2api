package responses

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/privatecontext"
	"whale2api/internal/promptcompat"
)

const (
	privateContextContentType = "text/plain; charset=utf-8"
	privateContextPurpose     = "assistants"
	privateContextLivePrompt  = "请自然延续对话，并直接回应用户的最新请求。"
)

type currentInputFileConfig interface {
	CurrentInputFileEnabled() bool
	CurrentInputFileMinChars() int
}

func (h *Handler) applyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	cfg, ok := h.Store.(currentInputFileConfig)
	if !ok || h.DS == nil || a == nil || !cfg.CurrentInputFileEnabled() {
		return stdReq, nil
	}
	if !hasLatestUserInputForPrivateContext(stdReq.Messages) {
		return stdReq, nil
	}

	contextText := promptcompat.BuildOpenAIPrivateContextTranscript(stdReq.Messages)
	if strings.TrimSpace(contextText) == "" {
		return stdReq, errors.New("private context transcript is empty")
	}
	if minChars := cfg.CurrentInputFileMinChars(); minChars > 0 && len([]rune(contextText)) < minChars {
		return stdReq, nil
	}

	modelType := "default"
	if resolvedType, ok := config.GetModelType(config.UpstreamDeepSeekSKU(stdReq.ResolvedModel)); ok {
		modelType = resolvedType
	}
	uploadReq := dsclient.UploadFileRequest{
		Filename:    privateContextFilename(),
		ContentType: privateContextContentType,
		Purpose:     privateContextPurpose,
		ModelType:   modelType,
		Data:        []byte(contextText),
	}
	result, err := privatecontext.Upload(ctx, h.DS, a, uploadReq, 3, privatecontext.Key(a, modelType, contextText))
	if err != nil {
		return stdReq, fmt.Errorf("upload private context: %w", err)
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return stdReq, errors.New("upload private context returned empty file id")
	}

	liveMessages := []any{
		map[string]any{
			"role":    "user",
			"content": privateContextLivePrompt,
		},
	}
	finalPrompt, toolNames := promptcompat.BuildOpenAIPrompt(liveMessages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	stdReq.HistoryText = contextText
	stdReq.FinalPrompt = finalPrompt
	stdReq.PromptTokenText = contextText + "\n" + finalPrompt
	stdReq.ToolNames = toolNames
	stdReq.RefFileIDs = prependUniqueRefFileID(stdReq.RefFileIDs, fileID)
	stdReq.RefFileTokens += len([]rune(contextText)) / 3
	stdReq.PrivateContextText = contextText
	stdReq.PrivateContextAccountID = strings.TrimSpace(a.AccountID)
	stdReq.PrivateContextRefFileID = fileID
	return stdReq, nil
}

func hasLatestUserInputForPrivateContext(messages []any) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		if role != "user" {
			continue
		}
		text := promptcompat.NormalizeOpenAIContentForPrompt(msg["content"])
		return strings.TrimSpace(text) != ""
	}
	return false
}

func privateContextFilename() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:]) + ".txt"
	}
	return "000000000000000000000000.txt"
}

func prependUniqueRefFileID(existing []string, fileID string) []string {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return existing
	}
	out := make([]string, 0, len(existing)+1)
	out = append(out, fileID)
	for _, id := range existing {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || strings.EqualFold(trimmed, fileID) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func mapCurrentInputFileError(err error) (int, string) {
	if err == nil {
		return http.StatusInternalServerError, "Failed to prepare request."
	}
	return http.StatusInternalServerError, "Failed to prepare request."
}
