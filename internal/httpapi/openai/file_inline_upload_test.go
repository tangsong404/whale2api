package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/auth"
	dsclient "whale2api/internal/deepseek/client"
)

type inlineUploadDSStub struct {
	uploadCalls    []dsclient.UploadFileRequest
	lastCtx        context.Context
	completionReq  map[string]any
	createSession  string
	uploadErr      error
	completionResp *http.Response
}

func (m *inlineUploadDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	if strings.TrimSpace(m.createSession) == "" {
		return "session-id", nil
	}
	return m.createSession, nil
}

func (m *inlineUploadDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (m *inlineUploadDSStub) UploadFile(ctx context.Context, _ *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	m.lastCtx = ctx
	m.uploadCalls = append(m.uploadCalls, req)
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return &dsclient.UploadFileResult{
		ID:       "file-inline-1",
		Filename: req.Filename,
		Bytes:    int64(len(req.Data)),
		Status:   "uploaded",
		Purpose:  req.Purpose,
	}, nil
}

func (m *inlineUploadDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	m.completionReq = payload
	if m.completionResp != nil {
		return m.completionResp, nil
	}
	return makeOpenAISSEHTTPResponse(
		`data: {"p":"response/content","v":"ok"}`,
		`data: [DONE]`,
	), nil
}

func (m *inlineUploadDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (m *inlineUploadDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestPreprocessInlineFileInputsReplacesDataURLAndCollectsRefFileIDs(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,QUJDRA=="},
					},
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.preprocessInlineFileInputs(ctx, &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected default model type when request omits model, got %q", ds.uploadCalls[0].ModelType)
	}
	if ds.lastCtx != ctx {
		t.Fatalf("expected upload to use request context")
	}
	if ds.uploadCalls[0].ContentType != "image/png" {
		t.Fatalf("expected image/png, got %q", ds.uploadCalls[0].ContentType)
	}
	if ds.uploadCalls[0].Filename != "image.png" {
		t.Fatalf("expected inferred filename image.png, got %q", ds.uploadCalls[0].Filename)
	}
	messages, _ := req["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	block, _ := content[0].(map[string]any)
	if block["type"] != "input_image" {
		t.Fatalf("expected input_image replacement, got %#v", block)
	}
	if block["file_id"] != "file-inline-1" {
		t.Fatalf("expected file-inline-1 replacement id, got %#v", block)
	}
	refIDs, _ := req["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-inline-1" {
		t.Fatalf("unexpected ref_file_ids: %#v", req["ref_file_ids"])
	}
}

func TestPreprocessInlineFileInputsSupportsCompactImageMediaTypeData(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"model": "deepseek-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image",
						"mediaType": "image/png",
						"data":      "QUJDRA==",
						"name":      "image.png",
					},
					map[string]any{
						"type": "text",
						"text": "这是啥",
					},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected default model type, got %q", ds.uploadCalls[0].ModelType)
	}
	if ds.uploadCalls[0].ContentType != "image/png" {
		t.Fatalf("expected image/png, got %q", ds.uploadCalls[0].ContentType)
	}
	if ds.uploadCalls[0].Filename != "image.png" {
		t.Fatalf("expected filename image.png, got %q", ds.uploadCalls[0].Filename)
	}
	if string(ds.uploadCalls[0].Data) != "ABCD" {
		t.Fatalf("expected decoded payload ABCD, got %q", ds.uploadCalls[0].Data)
	}
	messages, _ := req["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %#v", content)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "input_image" {
		t.Fatalf("expected input_image replacement, got %#v", block)
	}
	if block["file_id"] != "file-inline-1" {
		t.Fatalf("expected file-inline-1, got %#v", block)
	}
	textBlock, _ := content[1].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "这是啥" {
		t.Fatalf("expected text part preserved, got %#v", textBlock)
	}
}

func TestPreprocessInlineFileInputsSupportsFileDataImage(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "file",
						"file_data": "data:image/jpeg;base64,QUJDRA==",
						"filename":  "image.jpg",
					},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(ds.uploadCalls))
	}
	messages, _ := req["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	block, _ := content[0].(map[string]any)
	if block["type"] != "input_image" {
		t.Fatalf("expected image file_data to become input_image, got %#v", block)
	}
}

func TestPreprocessInlineFileInputsFetchesRemoteImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("remote-png"))
	}))
	defer server.Close()

	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": server.URL + "/pic.png", "detail": "low"},
					},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(ds.uploadCalls))
	}
	if string(ds.uploadCalls[0].Data) != "remote-png" {
		t.Fatalf("expected downloaded payload, got %q", ds.uploadCalls[0].Data)
	}
	if ds.uploadCalls[0].ContentType != "image/png" {
		t.Fatalf("expected image/png, got %q", ds.uploadCalls[0].ContentType)
	}
	messages, _ := req["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	block, _ := content[0].(map[string]any)
	if block["type"] != "input_image" || block["file_id"] != "file-inline-1" {
		t.Fatalf("expected input_image replacement, got %#v", block)
	}
}

func TestPreprocessInlineFileInputsIgnoresAnthropicAndResponsesImageShapes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer server.Close()

	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/jpeg",
							"data":       "QUJDRA==",
						},
					},
					map[string]any{
						"type":      "input_image",
						"image_url": map[string]any{"url": "data:image/png;base64,QUJDRA=="},
					},
					map[string]any{
						"type":      "input_image",
						"image_url": server.URL + "/a.png",
						"detail":    "low",
					},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no uploads for unsupported vision shapes, got %d", len(ds.uploadCalls))
	}
}

func TestPreprocessInlineFileInputsLeavesFileIDReference(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":    "file",
						"file_id": "file-api-xxxxxxxxxxxxxxxx",
					},
					map[string]any{
						"type":    "image_url",
						"file_id": "file-api-yyyyyyyyyyyyyyyy",
					},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no uploads for file_id refs, got %d", len(ds.uploadCalls))
	}
	refIDs, _ := req["ref_file_ids"].([]any)
	if len(refIDs) != 2 {
		t.Fatalf("expected both file_ids collected, got %#v", req["ref_file_ids"])
	}
}

func TestPreprocessInlineFileInputsDeduplicatesIdenticalPayloads(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{DS: ds}
	req := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,QUJDRA=="}},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,QUJDRA=="}},
				},
			},
		},
	}

	if err := h.preprocessInlineFileInputs(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, req); err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected deduplicated single upload, got %d", len(ds.uploadCalls))
	}
	refIDs, _ := req["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-inline-1" {
		t.Fatalf("unexpected ref_file_ids after dedupe: %#v", req["ref_file_ids"])
	}
}

func TestChatCompletionsUploadsInlineFilesBeforeCompletion(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	reqBody := `{"model":"deepseek-flash","messages":[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJDRA=="}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected default model type for flash request, got %q", ds.uploadCalls[0].ModelType)
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	refIDs, _ := ds.completionReq["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-inline-1" {
		t.Fatalf("unexpected completion ref_file_ids: %#v", ds.completionReq["ref_file_ids"])
	}
	if got := ds.completionReq["model_type"]; got != "default" {
		t.Fatalf("expected completion model_type default, got %#v", got)
	}
}

func TestChatCompletionsUploadsInlineFilesRejectsRetiredVisionModel(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	reqBody := `{"model":"deepseek-v4-flash-vision-exp","messages":[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJDRA=="}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.completionReq != nil {
		t.Fatal("did not expect completion call for retired model")
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected default model type, got %q", ds.uploadCalls[0].ModelType)
	}
}

func TestResponsesUploadsInlineFilesBeforeCompletion(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)
	reqBody := `{"model":"deepseek-flash","input":[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJDRA=="}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected default model type for flash request, got %q", ds.uploadCalls[0].ModelType)
	}
	refIDs, _ := ds.completionReq["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-inline-1" {
		t.Fatalf("unexpected completion ref_file_ids: %#v", ds.completionReq["ref_file_ids"])
	}
}

func TestChatCompletionsInlineUploadFailureReturnsBadRequest(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	reqBody := `{"model":"deepseek-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%%%"}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.completionReq != nil {
		t.Fatalf("did not expect completion call on upload decode error")
	}
}

func TestChatCompletionsInlineUploadLimitReturnsBadRequest(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	content := []any{map[string]any{"type": "input_text", "text": "hi"}}
	for i := 0; i < 51; i++ {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": "data:image/png;base64,QUJDRA=="},
		})
	}
	body, err := json.Marshal(map[string]any{
		"model": "deepseek-flash",
		"messages": []any{map[string]any{
			"role":    "user",
			"content": content,
		}},
		"stream": false,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "exceeded maximum of 50 inline files per request") {
		t.Fatalf("expected inline file limit error, got body=%s", rec.Body.String())
	}
	if ds.completionReq != nil {
		t.Fatalf("did not expect completion call after inline file limit error")
	}
}

func TestResponsesInlineUploadFailureReturnsInternalServerError(t *testing.T) {
	ds := &inlineUploadDSStub{uploadErr: errors.New("boom")}
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: ds}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)
	reqBody := `{"model":"deepseek-flash","input":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJDRA=="}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.completionReq != nil {
		t.Fatalf("did not expect completion call after upload failure")
	}
}
