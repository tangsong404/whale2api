package openai



import (

	"strings"

	"testing"



	"whale2api/internal/promptcompat"

)



type mockOpenAIConfig struct {

	autoDeleteMode      string

	toolMode            string

	earlyEmit           string

	responsesTTL        int

	embedProv           string

	thinkingInjection *bool

	thinkingPrompt      string

}



func (m mockOpenAIConfig) ToolcallMode() string                { return m.toolMode }

func (m mockOpenAIConfig) ToolcallEarlyEmitConfidence() string { return m.earlyEmit }

func (m mockOpenAIConfig) ResponsesStoreTTLSeconds() int       { return m.responsesTTL }

func (m mockOpenAIConfig) EmbeddingsProvider() string          { return m.embedProv }

func (m mockOpenAIConfig) AutoDeleteMode() string {

	if m.autoDeleteMode == "" {

		return "none"

	}

	return m.autoDeleteMode

}

func (m mockOpenAIConfig) AutoDeleteSessions() bool { return false }



func (m mockOpenAIConfig) ThinkingInjectionEnabled() bool {

	if m.thinkingInjection == nil {

		return false

	}

	return *m.thinkingInjection

}

func (m mockOpenAIConfig) ThinkingInjectionPrompt() string { return m.thinkingPrompt }



func TestNormalizeOpenAIChatRequestFlash(t *testing.T) {
	req := map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-flash" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if out.ResponseModel != "deepseek-v4-flash" {
		t.Fatalf("response model mismatch: got=%q", out.ResponseModel)
	}
	if out.Search || !out.Thinking {
		t.Fatalf("unexpected model flags: thinking=%v search=%v", out.Thinking, out.Search)
	}
}

func TestNormalizeOpenAIChatRequestRejectsPro(t *testing.T) {
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	_, err := promptcompat.NormalizeOpenAIChatRequest(req, "")
	if err == nil {
		t.Fatal("expected pro model to be rejected")
	}
}



func TestNormalizeOpenAIChatRequestRejectsAliasModel(t *testing.T) {

	req := map[string]any{

		"model":    "gpt-4.1",

		"messages": []any{map[string]any{"role": "user", "content": "hello"}},

	}

	_, err := promptcompat.NormalizeOpenAIChatRequest(req, "")

	if err == nil {

		t.Fatal("expected alias model to be rejected")

	}

}




func TestNormalizeOpenAIChatRequestRejectsNoThinkingModel(t *testing.T) {

	req := map[string]any{

		"model":    "deepseek-v4-flash-nothinking",

		"messages": []any{map[string]any{"role": "user", "content": "hello"}},

	}

	_, err := promptcompat.NormalizeOpenAIChatRequest(req, "")

	if err == nil {

		t.Fatal("expected nothinking model to be rejected")

	}

}



func TestNormalizeOpenAIResponsesRequestAlwaysAcceptsWideInput(t *testing.T) {

	req := map[string]any{

		"model": "deepseek-v4-flash",

		"input": "hi",

	}



	out, err := promptcompat.NormalizeOpenAIResponsesRequest(req, "")

	if err != nil {

		t.Fatalf("unexpected error for wide input request: %v", err)

	}

	if out.Surface != "openai_responses" {

		t.Fatalf("unexpected surface: %q", out.Surface)

	}

	if !strings.Contains(out.FinalPrompt, "<｜User｜>hi") {

		t.Fatalf("unexpected final prompt: %q", out.FinalPrompt)

	}

}


