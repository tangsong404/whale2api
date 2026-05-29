package promptcompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"whale2api/internal/toolcall"
)

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
			desc = "暂无说明"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("工具：%s\n说明：%s\n参数：%s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return messages, names
	}
	toolPrompt := "你可以使用以下工具：\n\n" + strings.Join(toolSchemas, "\n\n") + "\n\n" + toolcall.BuildToolCallInstructions(names)
	if hasReadLikeTool(names) {
		toolPrompt += "\n\n读取类工具缓存提示：若 Read/read_file 等工具返回表示文件未变更、内容已在历史上下文中、应从先前上下文引用，或没有给出文件正文，请将结果视为“内容缺失”。不要为获取缺失正文而反复发起相同的读取请求；若工具支持全文读取请改用相应方式，否则请明确告知用户需要重新提供文件内容。"
	}
	if policy.Mode == ToolChoiceRequired {
		toolPrompt += "\n7）在本回复中，你必须从允许列表里至少调用一个工具。"
	}
	if policy.Mode == ToolChoiceForced && strings.TrimSpace(policy.ForcedName) != "" {
		toolPrompt += "\n7）在本回复中，你必须且只能调用以下工具名称：" + strings.TrimSpace(policy.ForcedName)
		toolPrompt += "\n8）不要调用任何其它工具。"
	}

	for i := range messages {
		if messages[i]["role"] == "system" {
			old, _ := messages[i]["content"].(string)
			messages[i]["content"] = strings.TrimSpace(old + "\n\n" + toolPrompt)
			return messages, names
		}
	}
	messages = append([]map[string]any{{"role": "system", "content": toolPrompt}}, messages...)
	return messages, names
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
