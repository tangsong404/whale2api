package config

import "testing"

func TestResolveModelDirectDeepSeekFlash(t *testing.T) {
	got, ok := ResolveModel("deepseek-v4-flash")
	if !ok || got != "deepseek-v4-flash" {
		t.Fatalf("expected deepseek-v4-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelDirectDeepSeekFlashVisionExp(t *testing.T) {
	got, ok := ResolveModel("deepseek-v4-flash-vision-exp")
	if !ok || got != "deepseek-v4-flash-vision-exp" {
		t.Fatalf("expected deepseek-v4-flash-vision-exp, got ok=%v model=%q", ok, got)
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

func TestOpenAIModelByIDPreservesFlashVisionExpID(t *testing.T) {
	info, ok := OpenAIModelByID("deepseek-v4-flash-vision-exp")
	if !ok || info.ID != "deepseek-v4-flash-vision-exp" {
		t.Fatalf("expected advertised deepseek-v4-flash-vision-exp, got ok=%v id=%q", ok, info.ID)
	}
	if !info.Multimodal {
		t.Fatal("expected deepseek-v4-flash-vision-exp multimodal=true")
	}
	flash, ok := OpenAIModelByID("deepseek-v4-flash")
	if !ok || flash.Multimodal {
		t.Fatalf("expected deepseek-v4-flash multimodal=false, got ok=%v multimodal=%v", ok, flash.Multimodal)
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
	if got := UpstreamDeepSeekSKU("deepseek-v4-flash-vision-exp"); got != "deepseek-v4-flash-vision-exp" {
		t.Fatalf("unexpected sku: %q", got)
	}
}

func TestGetModelTypeVisionExp(t *testing.T) {
	got, ok := GetModelType("deepseek-v4-flash-vision-exp")
	if !ok || got != "vision" {
		t.Fatalf("expected vision, got ok=%v type=%q", ok, got)
	}
	got, ok = GetModelType("deepseek-v4-flash")
	if !ok || got != "default" {
		t.Fatalf("expected default, got ok=%v type=%q", ok, got)
	}
}

func TestGetModelConfigVisionExp(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-v4-flash-vision-exp")
	if !ok || !thinking || search {
		t.Fatalf("expected thinking=true search=false ok=true, got thinking=%v search=%v ok=%v", thinking, search, ok)
	}
}

func TestUpstreamSafeModelType(t *testing.T) {
	if got := UpstreamSafeModelType("vision"); got != "vision" {
		t.Fatalf("expected vision, got %q", got)
	}
	if got := UpstreamSafeModelType("default"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
	if got := UpstreamSafeModelType("expert"); got != "default" {
		t.Fatalf("expected default for expert, got %q", got)
	}
	if got := UpstreamSafeModelType(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
