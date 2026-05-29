package promptcompat

import "strings"

const (
	ThinkingInjectionMarker        = "推理强度：必须全力以赴，禁止走捷径或敷衍。"
	DefaultThinkingInjectionPrompt = ThinkingInjectionMarker + "\n" +
		"你必须充分、细致地思考：将问题拆解到根因，对所有可能路径、边界情形与对抗性场景做严格自检。\n" +
		"请完整写出推理过程，记录每一步中间结论、曾考虑的备选方案以及被否定的假设，确保不遗漏任何隐含前提。"
)

func AppendThinkingInjectionToLatestUser(messages []any) ([]any, bool) {
	return AppendThinkingInjectionPromptToLatestUser(messages, "")
}

func AppendThinkingInjectionPromptToLatestUser(messages []any, injectionPrompt string) ([]any, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	injectionPrompt = strings.TrimSpace(injectionPrompt)
	if injectionPrompt == "" {
		injectionPrompt = DefaultThinkingInjectionPrompt
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(asString(msg["role"]))) != "user" {
			continue
		}
		content := msg["content"]
		normalizedContent := NormalizeOpenAIContentForPrompt(content)
		if strings.Contains(normalizedContent, ThinkingInjectionMarker) || strings.Contains(normalizedContent, injectionPrompt) {
			return messages, false
		}
		updatedContent := appendThinkingInjectionToContent(content, injectionPrompt)
		out := append([]any(nil), messages...)
		cloned := make(map[string]any, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = updatedContent
		out[i] = cloned
		return out, true
	}
	return messages, false
}

func appendThinkingInjectionToContent(content any, injectionPrompt string) any {
	switch x := content.(type) {
	case string:
		return appendTextBlock(x, injectionPrompt)
	case []any:
		out := append([]any(nil), x...)
		out = append(out, map[string]any{
			"type": "text",
			"text": injectionPrompt,
		})
		return out
	default:
		text := NormalizeOpenAIContentForPrompt(content)
		return appendTextBlock(text, injectionPrompt)
	}
}

func appendTextBlock(base, addition string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return addition
	}
	return base + "\n\n" + addition
}
