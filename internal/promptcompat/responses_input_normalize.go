package promptcompat

import (
	"fmt"
	"strings"
)

func ResponsesMessagesFromRequest(req map[string]any) []any {
	if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
		return prependInstructionMessage(msgs, req["instructions"])
	}
	if rawInput, ok := req["input"]; ok {
		if msgs := NormalizeResponsesInputAsMessages(rawInput); len(msgs) > 0 {
			return prependInstructionMessage(msgs, req["instructions"])
		}
	}
	return nil
}

func prependInstructionMessage(messages []any, instructions any) []any {
	sys, _ := instructions.(string)
	sys = strings.TrimSpace(sys)
	if sys == "" {
		return messages
	}
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": sys})
	out = append(out, messages...)
	return out
}

func NormalizeResponsesInputAsMessages(input any) []any {
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []any{map[string]any{"role": "user", "content": v}}
	case []any:
		return normalizeResponsesInputArray(v)
	case map[string]any:
		if msg := normalizeResponsesInputItem(v); msg != nil {
			return []any{msg}
		}
		if txt, _ := v["text"].(string); strings.TrimSpace(txt) != "" {
			return []any{map[string]any{"role": "user", "content": txt}}
		}
		if content, ok := v["content"]; ok {
			if strings.TrimSpace(NormalizeOpenAIContentForPrompt(content)) != "" {
				return []any{map[string]any{"role": "user", "content": content}}
			}
		}
	}
	return nil
}

func normalizeResponsesInputArray(items []any) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	callNameByID := map[string]string{}
	fallbackParts := make([]string, 0, len(items))
	flushFallback := func() {
		if len(fallbackParts) == 0 {
			return
		}
		out = append(out, map[string]any{"role": "user", "content": strings.Join(fallbackParts, "\n")})
		fallbackParts = fallbackParts[:0]
	}

	for _, item := range items {
		switch x := item.(type) {
		case map[string]any:
			if msg := normalizeResponsesInputItemWithState(x, callNameByID); msg != nil {
				flushFallback()
				out = append(out, msg)
				continue
			}
			if s := normalizeResponsesFallbackPart(x); s != "" {
				fallbackParts = append(fallbackParts, s)
			}
		default:
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				fallbackParts = append(fallbackParts, s)
			}
		}
	}
	flushFallback()
	if len(out) == 0 {
		return nil
	}
	return out
}
