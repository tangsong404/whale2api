package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/promptcompat"
)

func newTestChatHistoryStore(t *testing.T) *chathistory.Store {
	t.Helper()
	store := chathistory.New(filepath.Join(t.TempDir(), "chat_history.json"))
	if err := store.Err(); err != nil {
		t.Fatalf("chat history store unavailable: %v", err)
	}
	return store
}

func blockChatHistoryDetailDir(t *testing.T, detailDir string) func() {
	t.Helper()
	blockedDir := detailDir + ".blocked"
	if err := os.RemoveAll(blockedDir); err != nil {
		t.Fatalf("remove blocked detail dir failed: %v", err)
	}
	if err := os.Rename(detailDir, blockedDir); err != nil {
		t.Fatalf("move detail dir aside failed: %v", err)
	}
	if err := os.RemoveAll(detailDir); err != nil {
		t.Fatalf("remove blocked detail path failed: %v", err)
	}
	if err := os.WriteFile(detailDir, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("write blocked detail path failed: %v", err)
	}
	var once sync.Once
	return func() {
		t.Helper()
		once.Do(func() {
			if err := os.RemoveAll(detailDir); err != nil {
				t.Fatalf("remove blocking detail path failed: %v", err)
			}
			if err := os.Rename(blockedDir, detailDir); err != nil {
				t.Fatalf("restore detail dir failed: %v", err)
			}
		})
	}
}

func TestChatCompletionsNonStreamPersistsHistory(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	h := &Handler{
		Store:       mockOpenAIConfig{},
		Auth:        streamStatusAuthStub{},
		DS:          streamStatusDSStub{resp: makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":"hello world"}`, `data: [DONE]`)},
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"be precise"},{"role":"user","content":"hi there"},{"role":"assistant","content":"previous answer"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	item := snapshot.Items[0]
	if item.Status != "success" || item.UserInput != "hi there" {
		t.Fatalf("unexpected persisted history summary: %#v", item)
	}
	full, err := historyStore.Get(item.ID)
	if err != nil {
		t.Fatalf("expected detail item, got %v", err)
	}
	if full.Content != "hello world" {
		t.Fatalf("expected detail content persisted, got %#v", full)
	}
	if len(full.Messages) != 3 {
		t.Fatalf("expected all request messages persisted, got %#v", full.Messages)
	}
	if full.FinalPrompt == "" {
		t.Fatalf("expected final prompt to be persisted")
	}
	if item.CallerID != "caller:test" {
		t.Fatalf("expected caller hash persisted in summary, got %#v", item.CallerID)
	}
}

func TestChatHistoryNonStreamArchivesRawToolCallMarkup(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	entry, err := historyStore.Start(chathistory.StartParams{
		CallerID:  "caller:test",
		Model:     "deepseek-v4-flash",
		UserInput: "call tool",
	})
	if err != nil {
		t.Fatalf("start history failed: %v", err)
	}
	session := &chatHistorySession{
		store:       historyStore,
		entryID:     entry.ID,
		startedAt:   time.Now(),
		lastPersist: time.Now().Add(-time.Second),
		finalPrompt: "call tool",
	}
	rawToolCall := `<tool_calls><invoke name="search"><parameter name="q">golang</parameter></invoke></tool_calls>`

	h := &Handler{}
	rec := httptest.NewRecorder()
	resp := makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":`+strconv.Quote(rawToolCall)+`}`, `data: [DONE]`)
	h.handleNonStream(rec, resp, "cid-tool-history", "deepseek-v4-flash", "prompt", 0, false, false, []string{"search"}, nil, session)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	full, err := historyStore.Get(entry.ID)
	if err != nil {
		t.Fatalf("get detail failed: %v", err)
	}
	if full.Content != rawToolCall {
		t.Fatalf("expected raw tool markup archived, got %q", full.Content)
	}
	if full.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", full.FinishReason)
	}
}

func TestChatHistoryStreamArchivesRawToolCallMarkup(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	entry, err := historyStore.Start(chathistory.StartParams{
		CallerID:  "caller:test",
		Model:     "deepseek-v4-flash",
		Stream:    true,
		UserInput: "call tool",
	})
	if err != nil {
		t.Fatalf("start history failed: %v", err)
	}
	session := &chatHistorySession{
		store:       historyStore,
		entryID:     entry.ID,
		startedAt:   time.Now(),
		lastPersist: time.Now().Add(-time.Second),
		finalPrompt: "call tool",
	}
	rawToolCall := `<tool_calls><invoke name="search"><parameter name="q">golang</parameter></invoke></tool_calls>`

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	resp := makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":`+strconv.Quote(rawToolCall)+`}`, `data: [DONE]`)
	h.handleStream(rec, req, resp, "cid-stream-tool-history", "deepseek-v4-flash", "prompt", 0, false, false, []string{"search"}, nil, session)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	full, err := historyStore.Get(entry.ID)
	if err != nil {
		t.Fatalf("get detail failed: %v", err)
	}
	if full.Content != rawToolCall {
		t.Fatalf("expected raw streamed tool markup archived, got %q", full.Content)
	}
	if full.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", full.FinishReason)
	}
}

func TestStartChatHistoryRecoversFromTransientWriteFailure(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	restore := blockChatHistoryDetailDir(t, historyStore.DetailDir())
	t.Cleanup(restore)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	a := &auth.RequestAuth{
		CallerID:  "caller:test",
		AccountID: "acct:test",
	}
	stdReq := promptcompat.StandardRequest{
		ResponseModel: "deepseek-v4-flash",
		Stream:        true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		FinalPrompt: "hello",
	}

	session := startChatHistory(historyStore, req, a, stdReq)
	if session == nil {
		t.Fatalf("expected session even when initial persistence fails")
		return
	}
	if session.disabled {
		t.Fatalf("expected session to remain active after transient start failure")
	}
	if session.entryID == "" {
		t.Fatalf("expected session entry id to be retained")
	}
	if err := historyStore.Err(); err != nil {
		t.Fatalf("transient start failure should not latch store error: %v", err)
	}

	session.lastPersist = time.Now().Add(-time.Second)
	session.progress("thinking", "partial")
	if session.disabled {
		t.Fatalf("expected session to remain active after transient update failure")
	}
	if session.entryID == "" {
		t.Fatalf("expected session entry id to remain set after update failure")
	}
	if err := historyStore.Err(); err != nil {
		t.Fatalf("transient update failure should not latch store error: %v", err)
	}

	restore()

	session.success(http.StatusOK, "thinking", "final answer", "stop", map[string]any{"total_tokens": 7})
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed after restore: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one persisted item after restore, got %#v", snapshot.Items)
	}
	full, err := historyStore.Get(session.entryID)
	if err != nil {
		t.Fatalf("get restored entry failed: %v", err)
	}
	if full.Status != "success" || full.Content != "final answer" {
		t.Fatalf("expected restored entry to persist final success, got %#v", full)
	}
}

func TestHandleStreamContextCancelledMarksHistoryStopped(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	entry, err := historyStore.Start(chathistory.StartParams{
		CallerID:  "caller:test",
		Model:     "deepseek-v4-flash",
		Stream:    true,
		UserInput: "hello",
	})
	if err != nil {
		t.Fatalf("start history failed: %v", err)
	}
	session := &chatHistorySession{
		store:       historyStore,
		entryID:     entry.ID,
		startedAt:   time.Now(),
		lastPersist: time.Now(),
		finalPrompt: "hello",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	resp := makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":"hello"}`, `data: [DONE]`)

	h.handleStream(rec, req, resp, "cid-stop", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, session)

	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	full, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get detail failed: %v", err)
	}
	if full.Status != "stopped" {
		t.Fatalf("expected stopped status, got %#v", full)
	}
}

func TestChatCompletionsRecordsAdminWebUISource(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	h := &Handler{
		Store:       mockOpenAIConfig{},
		Auth:        streamStatusAuthStub{},
		DS:          streamStatusDSStub{resp: makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":"hello world"}`, `data: [DONE]`)},
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi there"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Whale2-Source", "admin-webui-api-tester")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected admin webui source to be recorded, got %#v", snapshot.Items)
	}
}

func TestChatCompletionsSkipsHistoryWhenDisabled(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	if _, err := historyStore.SetLimit(chathistory.DisabledLimit); err != nil {
		t.Fatalf("disable history store failed: %v", err)
	}
	h := &Handler{
		Store:       mockOpenAIConfig{},
		Auth:        streamStatusAuthStub{},
		DS:          streamStatusDSStub{resp: makeOpenAISSEHTTPResponse(`data: {"p":"response/content","v":"hello world"}`, `data: [DONE]`)},
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi there"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("expected disabled history to stay empty, got %#v", snapshot.Items)
	}
}

func TestChatCompletionsUploadsPrivateContextWithoutPromptingAboutFile(t *testing.T) {
	enabled := true
	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			currentInputFile: &enabled,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"be precise"},{"role":"user","content":"first user turn"},{"role":"assistant","content":"previous answer"},{"role":"user","content":"latest user turn"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected one private context upload, got %d", len(ds.uploadCalls))
	}
	upload := ds.uploadCalls[0]
	if strings.Contains(strings.ToLower(upload.Filename), "history") || !strings.HasSuffix(upload.Filename, ".txt") {
		t.Fatalf("expected opaque .txt filename, got %q", upload.Filename)
	}
	uploadedText := string(upload.Data)
	for _, forbidden := range []string{"WHALE2API_HISTORY", "file", "attachment", "hidden context"} {
		if strings.Contains(strings.ToLower(uploadedText), strings.ToLower(forbidden)) {
			t.Fatalf("uploaded private context leaked transport wording %q in %q", forbidden, uploadedText)
		}
	}
	if !strings.Contains(uploadedText, "系统:\nbe precise") || !strings.Contains(uploadedText, "助手:\nprevious answer") || !strings.Contains(uploadedText, "用户:\nlatest user turn") {
		t.Fatalf("uploaded private context did not preserve transcript, got %q", uploadedText)
	}
	prompt, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(prompt, "请自然延续对话，并直接回应用户的最新请求。") {
		t.Fatalf("expected upstream prompt to use neutral continuation, got %q", prompt)
	}
	for _, contextText := range []string{"be precise", "first user turn", "previous answer", "latest user turn"} {
		if strings.Contains(prompt, contextText) {
			t.Fatalf("expected upstream prompt to exclude private context %q, got %q", contextText, prompt)
		}
	}
	for _, transportWord := range []string{"WHALE2API_HISTORY", "file", "attachment", "hidden context"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(transportWord)) {
			t.Fatalf("expected upstream prompt to avoid transport wording %q, got %q", transportWord, prompt)
		}
	}
	refIDs, _ := ds.completionReq["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-inline-1" {
		t.Fatalf("expected private context ref_file_ids, got %#v", ds.completionReq["ref_file_ids"])
	}
}

func TestChatCompletionsMovesPostUserToolHistoryToPrivateContext(t *testing.T) {
	enabled := true
	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			currentInputFile: &enabled,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"old system context"},{"role":"user","content":"find email parser language support"},{"role":"assistant","content":"I will search.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_code","arguments":{"query":"email parser language support"}}}]},{"role":"tool","tool_call_id":"call_1","name":"search_code","content":"found: parser supports en, zh"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected one private context upload, got %d", len(ds.uploadCalls))
	}
	uploadedText := string(ds.uploadCalls[0].Data)
	for _, want := range []string{
		"find email parser language support",
		"<|ZJML|工具调用>",
		"search_code",
		"email parser language support",
		"found: parser supports en, zh",
	} {
		if !strings.Contains(uploadedText, want) {
			t.Fatalf("expected private context to contain %q, got %q", want, uploadedText)
		}
	}
	prompt, _ := ds.completionReq["prompt"].(string)
	for _, moved := range []string{
		"find email parser language support",
		"<|ZJML|工具调用>",
		"email parser language support",
		"found: parser supports en, zh",
	} {
		if strings.Contains(prompt, moved) {
			t.Fatalf("expected live prompt to exclude moved context %q, got %q", moved, prompt)
		}
	}
	if !strings.Contains(prompt, "请自然延续对话，并直接回应用户的最新请求。") {
		t.Fatalf("expected live prompt to use neutral continuation, got %q", prompt)
	}
}
