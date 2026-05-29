package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/httpapi/openai/chat"
	"whale2api/internal/httpapi/openai/embeddings"
	"whale2api/internal/httpapi/openai/files"
	"whale2api/internal/httpapi/openai/responses"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/promptcompat"
)

type openAITestSurface struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	DS          shared.DeepSeekCaller
	ChatHistory *chathistory.Store

	chat       *chat.Handler
	responses  *responses.Handler
	files      *files.Handler
	embeddings *embeddings.Handler
	models     *shared.ModelsHandler
}

func (h *openAITestSurface) deps() shared.Deps {
	if h == nil {
		return shared.Deps{}
	}
	return shared.Deps{Store: h.Store, Auth: h.Auth, DS: h.DS, ChatHistory: h.ChatHistory}
}

func (h *openAITestSurface) chatHandler() *chat.Handler {
	if h.chat == nil {
		deps := h.deps()
		h.chat = &chat.Handler{Store: deps.Store, Auth: deps.Auth, DS: deps.DS, ChatHistory: deps.ChatHistory}
	}
	return h.chat
}

func (h *openAITestSurface) responsesHandler() *responses.Handler {
	if h.responses == nil {
		deps := h.deps()
		h.responses = &responses.Handler{Store: deps.Store, Auth: deps.Auth, DS: deps.DS, ChatHistory: deps.ChatHistory}
	}
	return h.responses
}

func (h *openAITestSurface) filesHandler() *files.Handler {
	if h.files == nil {
		deps := h.deps()
		h.files = &files.Handler{Store: deps.Store, Auth: deps.Auth, DS: deps.DS, ChatHistory: deps.ChatHistory}
	}
	return h.files
}

func (h *openAITestSurface) embeddingsHandler() *embeddings.Handler {
	if h.embeddings == nil {
		deps := h.deps()
		h.embeddings = &embeddings.Handler{Store: deps.Store, Auth: deps.Auth, DS: deps.DS, ChatHistory: deps.ChatHistory}
	}
	return h.embeddings
}

func (h *openAITestSurface) modelsHandler() *shared.ModelsHandler {
	if h.models == nil {
		h.models = &shared.ModelsHandler{Store: h.Store}
	}
	return h.models
}

func (h *openAITestSurface) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.chatHandler().ChatCompletions(w, r)
}

func (h *openAITestSurface) applyOpenAIRequestTransforms(_ context.Context, _ *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	if h == nil {
		return stdReq, nil
	}
	return shared.ApplyThinkingInjection(h.Store, stdReq), nil
}

func (h *openAITestSurface) preprocessInlineFileInputs(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	return h.filesHandler().PreprocessInlineFileInputs(ctx, a, req)
}

func registerOpenAITestRoutes(r chi.Router, h *openAITestSurface) {
	r.Get("/v1/models", h.modelsHandler().ListModels)
	r.Get("/v1/models/{model_id}", h.modelsHandler().GetModel)
	r.Post("/v1/chat/completions", h.chatHandler().ChatCompletions)
	r.Post("/v1/responses", h.responsesHandler().Responses)
	r.Get("/v1/responses/{response_id}", h.responsesHandler().GetResponseByID)
	r.Post("/v1/files", h.filesHandler().UploadFile)
	r.Get("/v1/files/{file_id}", h.filesHandler().RetrieveFile)
	r.Post("/v1/embeddings", h.embeddingsHandler().Embeddings)
}

func boolPtr(v bool) *bool {
	b := v
	return &b
}

func TestApplyThinkingInjectionAppendsLatestUserPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			thinkingInjection: boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyOpenAIRequestTransforms(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply transforms failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload for first short turn, got %d", len(ds.uploadCalls))
	}
	if !strings.Contains(out.FinalPrompt, "hello\n\n"+promptcompat.ThinkingInjectionMarker) {
		t.Fatalf("expected thinking injection after latest user message, got %s", out.FinalPrompt)
	}
}

func TestApplyThinkingInjectionUsesCustomPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			thinkingInjection: boolPtr(true),
			thinkingPrompt:    "custom thinking format",
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyOpenAIRequestTransforms(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply transforms failed: %v", err)
	}
	if !strings.Contains(out.FinalPrompt, "hello\n\ncustom thinking format") {
		t.Fatalf("expected custom thinking injection after latest user message, got %s", out.FinalPrompt)
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	shared.WriteOpenAIError(w, status, message)
}

func replaceCitationMarkersWithLinks(text string, links map[int]string) string {
	return shared.ReplaceCitationMarkersWithLinks(text, links)
}

func sanitizeLeakedOutput(text string) string {
	return shared.CleanVisibleOutput(text, false)
}

func requestTraceID(r *http.Request) string {
	return shared.RequestTraceID(r)
}

func asString(v any) string {
	return shared.AsString(v)
}

func parseSSEDataFrames(t *testing.T, body string) ([]map[string]any, bool) {
	t.Helper()
	lines := strings.Split(body, "\n")
	frames := make([]map[string]any, 0, len(lines))
	done := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("decode sse frame failed: %v, payload=%s", err, payload)
		}
		frames = append(frames, frame)
	}
	return frames, done
}
