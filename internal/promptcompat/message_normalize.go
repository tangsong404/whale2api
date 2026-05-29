package promptcompat

import (
	"strings"

	"whale2api/internal/prompt"
)

const assistantReasoningLabel = "推理内容"

func NormalizeOpenAIMessagesForPrompt(raw []any, traceID string) []map[string]any {
	_ = traceID
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		switch role {
		case "assistant":
			content := buildAssistantContentForPrompt(msg)
			if content == "" {
				continue
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": content,
			})
		case "tool", "function":
			content := buildToolContentForPrompt(msg)
			out = append(out, map[string]any{
				"role":    "tool",
				"content": content,
			})
		case "user", "system", "developer":
			out = append(out, map[string]any{
				"role":    normalizeOpenAIRoleForPrompt(role),
				"content": NormalizeOpenAIContentForPrompt(msg["content"]),
			})
		default:
			content := NormalizeOpenAIContentForPrompt(msg["content"])
			if content == "" {
				continue
			}
			if role == "" {
				role = "user"
			}
			out = append(out, map[string]any{
				"role":    normalizeOpenAIRoleForPrompt(role),
				"content": content,
			})
		}
	}
	return out
}

func buildAssistantContentForPrompt(msg map[string]any) string {
	content := strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	reasoning := strings.TrimSpace(normalizeOpenAIReasoningContentForPrompt(msg["reasoning_content"]))
	if reasoning == "" {
		reasoning = strings.TrimSpace(extractOpenAIReasoningContentFromMessage(msg["content"]))
	}
	toolHistory := prompt.FormatToolCallsForPrompt(msg["tool_calls"])
	parts := make([]string, 0, 3)
	if reasoning != "" {
		parts = append(parts, formatPromptLabeledBlock(assistantReasoningLabel, reasoning))
	}
	if content != "" {
		parts = append(parts, content)
	}
	if toolHistory != "" {
		parts = append(parts, toolHistory)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts, "\n\n")
	}
}

func normalizeOpenAIReasoningContentForPrompt(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		return strings.Join(extractOpenAIReasoningPartsFromItems(x), "\n")
	case map[string]any:
		return extractOpenAIReasoningTextFromItem(x)
	default:
		return ""
	}
}

func extractOpenAIReasoningContentFromMessage(v any) string {
	switch x := v.(type) {
	case []any:
		return strings.Join(extractOpenAIReasoningPartsFromItems(x), "\n")
	case map[string]any:
		return extractOpenAIReasoningTextFromItem(x)
	default:
		return ""
	}
}

func extractOpenAIReasoningPartsFromItems(items []any) []string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if text := extractOpenAIReasoningTextFromItemMap(item); text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

func extractOpenAIReasoningTextFromItemMap(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	return extractOpenAIReasoningTextFromItem(m)
}

func extractOpenAIReasoningTextFromItem(m map[string]any) string {
	if m == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(asString(m["type"]))) {
	case "reasoning", "thinking":
		for _, key := range []string{"text", "thinking", "content"} {
			if text := strings.TrimSpace(asString(m[key])); text != "" {
				return text
			}
		}
	}
	return ""
}

func formatPromptLabeledBlock(label, text string) string {
	label = strings.TrimSpace(label)
	text = strings.TrimSpace(text)
	if label == "" {
		return text
	}
	return "[" + label + "]\n" + text + "\n[/" + label + "]"
}

func buildToolContentForPrompt(msg map[string]any) string {
	content := NormalizeOpenAIContentForPrompt(msg["content"])
	if strings.TrimSpace(content) == "" {
		return "null"
	}
	return content
}

func NormalizeOpenAIContentForPrompt(v any) string {
	return prompt.NormalizeContent(v)
}

func normalizeOpenAIRoleForPrompt(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "developer" {
		return "system"
	}
	return role
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
