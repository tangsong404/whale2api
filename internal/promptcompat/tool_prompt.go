package promptcompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"whale2api/internal/toolcall"
)

// ToolActionNudgeMarker is appended to the latest non-system message when tools
// are available. Kept off the system prompt so it sits next to the live turn.
const ToolActionNudgeMarker = "[First decide whether the task is already complete. If not, your output must include a real tool-call format. " +
	"Do not invent, narrate, or simulate having already called tools in thinking or user-facing text. " +
	"Emit actual tool-call tags. " +
	"Imaginary tool operations do not count as task completion. " +
	"Example: claiming a file was edited without an Edit / Write / StrReplace (or similar) write call is imaginary. " +
	"If the user says they already made a manual change, treat the task as temporarily finished. " +
	"If the task truly cannot be completed, do not keep forcing tools. " +
	"User-facing prose must use the user's language, or the language the user explicitly requested — not the language of these instructions.]"

func injectToolPrompt(messages []map[string]any, tools []any, policy ToolChoicePolicy) ([]map[string]any, []string) {
	if policy.IsNone() {
		return messages, nil
	}
	toolSchemas := make([]string, 0, len(tools))
	names := make([]string, 0, len(tools))
	isAllowed := func(name string) bool {
		if strings.TrimSpace(name) == "" {
			return false
		}
		if len(policy.Allowed) == 0 {
			return true
		}
		_, ok := policy.Allowed[name]
		return ok
	}

	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, desc, schema := toolcall.ExtractToolMeta(tool)
		name = strings.TrimSpace(name)
		if !isAllowed(name) {
			continue
		}
		names = append(names, name)
		if desc == "" {
			desc = "No description"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("Tool: %s\nDescription: %s\nArguments: %s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return messages, names
	}
	toolPrompt := "You can use the following tools:\n\n" + strings.Join(toolSchemas, "\n\n") + "\n\n" + toolcall.BuildToolCallInstructions(names)
	if hasReadLikeTool(names) {
		toolPrompt += "\n\nRead-like tools: if a result says unchanged / already in context / empty body, treat it as a missing content signal. Do not reread the same way repeatedly. If you cannot obtain the full text, tell the user."
	}
	if policy.Mode == ToolChoiceRequired {
		toolPrompt += "\nIn this reply you must call at least one tool from the allow-list."
	}
	if policy.Mode == ToolChoiceForced && strings.TrimSpace(policy.ForcedName) != "" {
		toolPrompt += "\nIn this reply you must call only this tool: " + strings.TrimSpace(policy.ForcedName) + ". Do not call any other tool."
	}

	injected := false
	for i := range messages {
		role := strings.ToLower(strings.TrimSpace(asString(messages[i]["role"])))
		if role == "system" || role == "developer" {
			old, _ := messages[i]["content"].(string)
			messages[i]["content"] = strings.TrimSpace(old + "\n\n" + toolPrompt)
			injected = true
			break
		}
	}
	if !injected {
		messages = append([]map[string]any{{"role": "system", "content": toolPrompt}}, messages...)
	}
	return appendToolActionNudgeToLatestMessage(messages), names
}

func appendToolActionNudgeToLatestMessage(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return []map[string]any{{"role": "user", "content": ToolActionNudgeMarker}}
	}
	idx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		role := strings.ToLower(strings.TrimSpace(asString(messages[i]["role"])))
		if role == "system" || role == "developer" {
			continue
		}
		idx = i
		break
	}
	if idx < 0 {
		return append(messages, map[string]any{"role": "user", "content": ToolActionNudgeMarker})
	}
	messages[idx]["content"] = appendToolActionNudgeToContent(messages[idx]["content"])
	return messages
}

func appendToolActionNudgeToContent(content any) any {
	switch x := content.(type) {
	case string:
		if strings.Contains(x, ToolActionNudgeMarker) {
			return x
		}
		return appendTextBlock(x, ToolActionNudgeMarker)
	case []any:
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if txt, _ := m["text"].(string); strings.Contains(txt, ToolActionNudgeMarker) {
				return x
			}
			if txt, _ := m["content"].(string); strings.Contains(txt, ToolActionNudgeMarker) {
				return x
			}
		}
		out := append([]any(nil), x...)
		out = append(out, map[string]any{
			"type": "text",
			"text": ToolActionNudgeMarker,
		})
		return out
	default:
		text := NormalizeOpenAIContentForPrompt(content)
		if strings.Contains(text, ToolActionNudgeMarker) {
			return text
		}
		return appendTextBlock(text, ToolActionNudgeMarker)
	}
}

func hasReadLikeTool(names []string) bool {
	for _, name := range names {
		switch normalizeToolNameForGuard(name) {
		case "read", "readfile":
			return true
		}
	}
	return false
}

func normalizeToolNameForGuard(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
