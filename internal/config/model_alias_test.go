package config

import "testing"

func TestResolveModelDirectDeepSeekFlash(t *testing.T) {
	got, ok := ResolveModel("deepseek-flash")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelRejectsRetiredFlashIDs(t *testing.T) {
	for _, model := range []string{
		"deepseek-v4-flash",
		"deepseek-v4-flash-vision-exp",
	} {
		if got, ok := ResolveModel(model); ok {
			t.Fatalf("expected retired id %q to be rejected, got %q", model, got)
		}
	}
}

func TestResolveModelRejectsPro(t *testing.T) {
	if got, ok := ResolveModel("deepseek-v4-pro"); ok {
		t.Fatalf("expected deepseek-v4-pro to be rejected, got %q", got)
	}
}

func TestResolveModelRejectsNoThinkingSuffix(t *testing.T) {
	for _, model := range []string{"deepseek-flash-nothinking", "deepseek-v4-pro-nothinking"} {
		if got, ok := ResolveModel(model); ok {
			t.Fatalf("expected %q to be rejected, got %q", model, got)
		}
	}
}

func TestDeepSeekModelsExposesSingleMultimodalFlash(t *testing.T) {
	if len(DeepSeekModels) != 1 {
		t.Fatalf("expected exactly 1 advertised model, got %d", len(DeepSeekModels))
	}
	model := DeepSeekModels[0]
	if model.ID != "deepseek-flash" {
		t.Fatalf("expected deepseek-flash, got %q", model.ID)
	}
	if !model.Multimodal {
		t.Fatal("expected deepseek-flash multimodal=true")
	}
	if model.ContextLength != AdvertisedMaxContextTokens {
		t.Fatalf("expected context_length=%d, got %d", AdvertisedMaxContextTokens, model.ContextLength)
	}
}

func TestOpenAIModelByIDDeepSeekFlash(t *testing.T) {
	info, ok := OpenAIModelByID("deepseek-flash")
	if !ok || info.ID != "deepseek-flash" {
		t.Fatalf("expected advertised deepseek-flash, got ok=%v id=%q", ok, info.ID)
	}
	if !info.Multimodal {
		t.Fatal("expected deepseek-flash multimodal=true")
	}
}

func TestOpenAIModelByIDRejectsRetiredIDs(t *testing.T) {
	for _, model := range []string{
		"deepseek-v4-flash",
		"deepseek-v4-flash-vision-exp",
	} {
		if info, ok := OpenAIModelByID(model); ok {
			t.Fatalf("expected %q to be rejected, got id=%q", model, info.ID)
		}
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
	if got := UpstreamDeepSeekSKU("deepseek-flash"); got != "deepseek-flash" {
		t.Fatalf("unexpected sku: %q", got)
	}
}

func TestGetModelType(t *testing.T) {
	got, ok := GetModelType("deepseek-flash")
	if !ok || got != "default" {
		t.Fatalf("expected default, got ok=%v type=%q", ok, got)
	}
	for _, retired := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		if got, ok := GetModelType(retired); ok {
			t.Fatalf("expected %q to be rejected, got ok=%v type=%q", retired, ok, got)
		}
	}
}

func TestGetModelConfig(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-flash")
	if !ok || !thinking || search {
		t.Fatalf("expected thinking=true search=false ok=true, got thinking=%v search=%v ok=%v", thinking, search, ok)
	}
}

func TestUpstreamSafeModelType(t *testing.T) {
	cases := map[string]string{
		"vision":  "default",
		"default": "default",
		"expert":  "default",
		"":        "",
	}
	for in, want := range cases {
		if got := UpstreamSafeModelType(in); got != want {
			t.Fatalf("UpstreamSafeModelType(%q) = %q, want %q", in, got, want)
		}
	}
}
