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
const ToolActionNudgeMarker = "【まずタスクが完了したか判断すること。未完了なら、出力に本物のツール呼び出し形式を必ず含めること。" +
	"思考過程やユーザー向け本文の中で、ツールを既に呼んだかのように空想・叙述・模擬してはならない。" +
	"ツール呼び出しタグを実際に出すこと。" +
	"空想上のツール操作による空想上の完了は、タスク完了とみなさない。" +
	"例：ファイルを編集したと宣言したのに Edit / Write / StrReplace 等の書き込み系呼び出しがなければ、それは空想である。】"

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
			desc = "説明なし"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("ツール：%s\n説明：%s\n引数：%s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return messages, names
	}
	toolPrompt := "次のツールを使用できる：\n\n" + strings.Join(toolSchemas, "\n\n") + "\n\n" + toolcall.BuildToolCallInstructions(names)
	if hasReadLikeTool(names) {
		toolPrompt += "\n\n読み取り系ツール：未変更・本文は文脈済み・本文なし、と返ってきた場合は内容欠落として扱う。同じ方法で繰り返し読まないこと。全文が取れないときはユーザーに知らせること。"
	}
	if policy.Mode == ToolChoiceRequired {
		toolPrompt += "\nこの返信では、許可リストから少なくとも1つのツールを必ず呼び出すこと。"
	}
	if policy.Mode == ToolChoiceForced && strings.TrimSpace(policy.ForcedName) != "" {
		toolPrompt += "\nこの返信では、次のツールのみを必ず呼び出すこと：" + strings.TrimSpace(policy.ForcedName) + "。他のツールは呼び出さないこと。"
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
