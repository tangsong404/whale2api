package config

import "strings"

type ModelInfo struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	ContextLength int    `json:"context_length"`
	Permission    []any  `json:"permission,omitempty"`
}

const noThinkingModelSuffix = "-nothinking"

const modelIDDeepSeekFlash = "deepseek-v4-flash"

// AdvertisedMaxContextTokens is the max context window advertised in /v1/models.
const AdvertisedMaxContextTokens = 256_000

var deepSeekBaseModels = []ModelInfo{
	{ID: modelIDDeepSeekFlash, Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
}

// DeepSeekModels lists client-visible model ids (flash only).
var DeepSeekModels = deepSeekBaseModels

func init() {
	for i := range DeepSeekModels {
		DeepSeekModels[i].ContextLength = AdvertisedMaxContextTokens
	}
}

func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	if hasNoThinkingSuffix(model) {
		return false, false, false
	}
	if internalBaseModel(baseModelID(model)) != modelIDDeepSeekFlash {
		return false, false, false
	}
	return true, false, true
}

func GetModelType(model string) (modelType string, ok bool) {
	if hasNoThinkingSuffix(model) {
		return "", false
	}
	if internalBaseModel(baseModelID(model)) == modelIDDeepSeekFlash {
		return "default", true
	}
	return "", false
}

func UpstreamDeepSeekSKU(resolvedModel string) string {
	if resolved, ok := ResolveModel(resolvedModel); ok {
		return resolved
	}
	return resolvedModel
}

func UpstreamSafeModelType(modelType string) string {
	if strings.TrimSpace(modelType) == "" {
		return ""
	}
	return "default"
}

// IsNoThinkingModel is kept for API compatibility; -nothinking model ids are rejected outright.
func IsNoThinkingModel(model string) bool {
	return false
}

// ResolveModel accepts only deepseek-v4-flash.
func ResolveModel(requested string) (string, bool) {
	if hasNoThinkingSuffix(requested) {
		return "", false
	}
	base := baseModelID(requested)
	internal := internalBaseModel(base)
	if internal != modelIDDeepSeekFlash {
		return "", false
	}
	return internal, true
}

func baseModelID(model string) string {
	id := lower(strings.TrimSpace(model))
	if strings.HasSuffix(id, noThinkingModelSuffix) {
		return strings.TrimSuffix(id, noThinkingModelSuffix)
	}
	return id
}

func hasNoThinkingSuffix(model string) bool {
	return strings.HasSuffix(lower(strings.TrimSpace(model)), noThinkingModelSuffix)
}

func internalBaseModel(baseModel string) string {
	return lower(strings.TrimSpace(baseModel))
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": DeepSeekModels}
}

func OpenAIModelByID(id string) (ModelInfo, bool) {
	if _, ok := ResolveModel(id); !ok {
		return ModelInfo{}, false
	}
	requested := lower(strings.TrimSpace(id))
	for _, model := range DeepSeekModels {
		if lower(model.ID) == requested {
			return model, true
		}
	}
	return ModelInfo{}, false
}
