package config

import "testing"

func TestResolveModelDirectDeepSeekFlash(t *testing.T) {
	got, ok := ResolveModel("deepseek-v4-flash")
	if !ok || got != "deepseek-v4-flash" {
		t.Fatalf("expected deepseek-v4-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelRejectsPro(t *testing.T) {
	if got, ok := ResolveModel("deepseek-v4-pro"); ok {
		t.Fatalf("expected deepseek-v4-pro to be rejected, got %q", got)
	}
}

func TestResolveModelRejectsNoThinkingSuffix(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash-nothinking", "deepseek-v4-pro-nothinking"} {
		if got, ok := ResolveModel(model); ok {
			t.Fatalf("expected %q to be rejected, got %q", model, got)
		}
	}
}

func TestOpenAIModelByIDPreservesFlashID(t *testing.T) {
	info, ok := OpenAIModelByID("deepseek-v4-flash")
	if !ok || info.ID != "deepseek-v4-flash" {
		t.Fatalf("expected advertised deepseek-v4-flash, got ok=%v id=%q", ok, info.ID)
	}
}

func TestResolveModelRejectsUnknownAliases(t *testing.T) {
	for _, model := range []string{
		"gpt-4.1",
		"gpt-4o",
		"claude-sonnet-4-6",
		"gemini-2.5-pro",
		"deepseek-chat",
		"deepseek-v4-pro",
	} {
		if got, ok := ResolveModel(model); ok {
			t.Fatalf("expected %q to be rejected, got %q", model, got)
		}
	}
}

func TestUpstreamDeepSeekSKU(t *testing.T) {
	if got := UpstreamDeepSeekSKU("deepseek-v4-flash"); got != "deepseek-v4-flash" {
		t.Fatalf("unexpected sku: %q", got)
	}
}

func TestUpstreamSafeModelType(t *testing.T) {
	if got := UpstreamSafeModelType("vision"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
	if got := UpstreamSafeModelType(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
