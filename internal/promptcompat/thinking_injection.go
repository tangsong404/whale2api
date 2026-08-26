package promptcompat

import "strings"

const (
	ThinkingInjectionMarker        = "Reasoning strength: give full effort; shortcuts and half-measures are forbidden."
	DefaultThinkingInjectionPrompt = ThinkingInjectionMarker + "\n" +
		"Break the problem down to root causes. Strictly inspect likely paths, edge cases, and adversarial cases. Keep reasoning in the thinking process only; do not put monologue in the final user-facing answer."
)

// AppendThinkingInjectionToLatestUser is retained for callers; it injects into system.
func AppendThinkingInjectionToLatestUser(messages []any) ([]any, bool) {
	return AppendThinkingInjectionPrompt(messages, "")
}

// AppendThinkingInjectionPromptToLatestUser is retained for callers; it injects into system.
func AppendThinkingInjectionPromptToLatestUser(messages []any, injectionPrompt string) ([]any, bool) {
	return AppendThinkingInjectionPrompt(messages, injectionPrompt)
}

// AppendThinkingInjectionPrompt appends the thinking-strength instruction to the
// first system/developer message (or creates a system message when none exist).
func AppendThinkingInjectionPrompt(messages []any, injectionPrompt string) ([]any, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	injectionPrompt = strings.TrimSpace(injectionPrompt)
	if injectionPrompt == "" {
		injectionPrompt = DefaultThinkingInjectionPrompt
	}

	sysIdx := -1
	for i, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if !isSystemRoleForPrompt(asString(msg["role"])) {
			continue
		}
		if sysIdx < 0 {
			sysIdx = i
		}
		content := NormalizeOpenAIContentForPrompt(msg["content"])
		if strings.Contains(content, ThinkingInjectionMarker) || strings.Contains(content, injectionPrompt) {
			return messages, false
		}
	}

	out := append([]any(nil), messages...)
	if sysIdx >= 0 {
		msg := out[sysIdx].(map[string]any)
		cloned := make(map[string]any, len(msg)+1)
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = appendThinkingInjectionToContent(msg["content"], injectionPrompt)
		out[sysIdx] = cloned
		return out, true
	}

	live := make([]any, 0, len(out)+1)
	live = append(live, map[string]any{"role": "system", "content": injectionPrompt})
	live = append(live, out...)
	return live, true
}

func isSystemRoleForPrompt(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return true
	default:
		return false
	}
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
