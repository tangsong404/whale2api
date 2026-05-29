package responses

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
)

type responsesHistoryDS struct {
	payload map[string]any
	uploads []dsclient.UploadFileRequest
}

func (d *responsesHistoryDS) CreateSession(context.Context, *auth.RequestAuth, int) (string, error) {
	return "session-id", nil
}

func (d *responsesHistoryDS) GetPow(context.Context, *auth.RequestAuth, int) (string, error) {
	return "pow", nil
}

func (d *responsesHistoryDS) UploadFile(_ context.Context, _ *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	d.uploads = append(d.uploads, req)
	return &dsclient.UploadFileResult{ID: "file-responses-context"}, nil
}

func (d *responsesHistoryDS) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	d.payload = payload
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"ok\"}\n")),
	}, nil
}

func TestResponsesUploadsPrivateContextAndReplacesLiveTail(t *testing.T) {
	store, resolver := newManagedKeyResolver(t)
	if err := store.Update(func(c *config.Config) error {
		c.CurrentInputFile.MinChars = 1
		return nil
	}); err != nil {
		t.Fatalf("set current input min chars: %v", err)
	}
	historyStore := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	ds := &responsesHistoryDS{}
	h := &Handler{
		Store:       store,
		Auth:        resolver,
		DS:          ds,
		ChatHistory: historyStore,
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"old response context"},{"role":"user","content":"find response language support"},{"role":"assistant","content":"I will search.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_code","arguments":{"query":"response language support"}}}]},{"role":"tool","tool_call_id":"call_1","name":"search_code","content":"found: responses supports en, zh"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer managed-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploads) != 1 {
		t.Fatalf("expected one private context upload, got %d", len(ds.uploads))
	}
	if !strings.HasSuffix(ds.uploads[0].Filename, ".txt") || strings.Contains(strings.ToLower(ds.uploads[0].Filename), "history") {
		t.Fatalf("expected opaque .txt private context filename, got %q", ds.uploads[0].Filename)
	}
	if got := string(ds.uploads[0].Data); !strings.Contains(got, "old response context") || !strings.Contains(got, "find response language support") || !strings.Contains(got, "found: responses supports en, zh") {
		t.Fatalf("expected upload to contain full private context, got %q", got)
	}
	prompt, _ := ds.payload["prompt"].(string)
	for _, moved := range []string{
		"find response language support",
		"<|ZJML|工具调用>",
		"response language support",
		"found: responses supports en, zh",
	} {
		if strings.Contains(prompt, moved) {
			t.Fatalf("expected live prompt to exclude moved context %q, got %q", moved, prompt)
		}
	}
	if !strings.Contains(prompt, "请自然延续对话，并直接回应用户的最新请求。") {
		t.Fatalf("expected live prompt to use neutral continuation, got %q", prompt)
	}
	refIDs, _ := ds.payload["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-responses-context" {
		t.Fatalf("expected uploaded private context ref id, got %#v", ds.payload["ref_file_ids"])
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot history: %v", err)
	}
	full, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get history item: %v", err)
	}
	if full.HistoryText != string(ds.uploads[0].Data) {
		t.Fatalf("expected response history to persist uploaded private context")
	}
}

func (d *responsesHistoryDS) DeleteSessionForToken(context.Context, string, string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (d *responsesHistoryDS) DeleteAllSessionsForToken(context.Context, string) error {
	return nil
}

func TestResponsesRecordsResponseHistory(t *testing.T) {
	store, resolver := newManagedKeyResolver(t)
	historyStore := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	ds := &responsesHistoryDS{}
	h := &Handler{
		Store:       store,
		Auth:        resolver,
		DS:          ds,
		ChatHistory: historyStore,
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"deepseek-v4-flash","input":"hello responses"}`))
	req.Header.Set("Authorization", "Bearer managed-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.payload == nil {
		t.Fatalf("expected upstream payload to be sent")
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot history: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	item, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get history item: %v", err)
	}
	if item.Surface != "openai.responses" {
		t.Fatalf("unexpected surface: %q", item.Surface)
	}
	if !strings.Contains(item.UserInput, "hello responses") {
		t.Fatalf("unexpected user input: %q", item.UserInput)
	}
	if strings.TrimSpace(item.HistoryText) != "" {
		t.Fatalf("expected empty persisted history text below current input threshold, got %q", item.HistoryText)
	}
	if item.Content != "ok" {
		t.Fatalf("expected raw upstream content, got %q", item.Content)
	}
}
