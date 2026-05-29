package promptcompat

import (
	"fmt"
	"strings"
)

func BuildOpenAIPrivateContextTranscript(messages []any) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	entry := 0
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := normalizeOpenAIRoleForPrompt(strings.ToLower(strings.TrimSpace(asString(msg["role"]))))
		content := strings.TrimSpace(buildOpenAIPrivateContextEntry(role, msg))
		if content == "" {
			continue
		}
		entry++
		fmt.Fprintf(&b, "%s:\n%s\n\n", privateContextRoleLabel(role), content)
	}
	transcript := strings.TrimSpace(b.String())
	if transcript == "" {
		return ""
	}
	return transcript + "\n"
}

func buildOpenAIPrivateContextEntry(role string, msg map[string]any) string {
	switch role {
	case "assistant":
		return strings.TrimSpace(buildAssistantContentForPrompt(msg))
	case "tool", "function":
		return strings.TrimSpace(buildPrivateContextToolContent(msg))
	case "system", "user":
		return strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	default:
		return strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	}
}

func buildPrivateContextToolContent(msg map[string]any) string {
	content := strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	parts := make([]string, 0, 2)
	if name := strings.TrimSpace(asString(msg["name"])); name != "" {
		parts = append(parts, "name="+name)
	}
	if callID := strings.TrimSpace(asString(msg["tool_call_id"])); callID != "" {
		parts = append(parts, "tool_call_id="+callID)
	}
	header := ""
	if len(parts) > 0 {
		header = "[" + strings.Join(parts, " ") + "]"
	}
	switch {
	case header != "" && content != "":
		return header + "\n" + content
	case header != "":
		return header
	default:
		return content
	}
}

func privateContextRoleLabel(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "function":
		return "工具"
	case "system":
		return "系统"
	case "assistant":
		return "助手"
	case "tool":
		return "工具"
	case "user":
		return "用户"
	case "":
		return "用户"
	default:
		return strings.ToUpper(role[:1]) + role[1:]
	}
}
