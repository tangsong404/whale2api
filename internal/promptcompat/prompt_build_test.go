package promptcompat

import (
	"strings"
	"testing"
)

func TestBuildOpenAIFinalPrompt_HandlerPathIncludesToolRoundtripSemantics(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "查北京天气"},
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{
					"id": "call_1",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": "{\"city\":\"beijing\"}",
					},
				},
			},
		},
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"name":         "get_weather",
			"content":      map[string]any{"temp": 18, "condition": "sunny"},
		},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, toolNames := buildOpenAIFinalPrompt(messages, tools, "", false)
	if len(toolNames) != 1 || toolNames[0] != "get_weather" {
		t.Fatalf("unexpected tool names: %#v", toolNames)
	}
	if !strings.Contains(finalPrompt, `"condition":"sunny"`) {
		t.Fatalf("handler finalPrompt should preserve tool output content: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "<|ZJML|工具调用>") {
		t.Fatalf("handler finalPrompt should preserve assistant tool history: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<|ZJML|调用项 name="get_weather">`) {
		t.Fatalf("handler finalPrompt should include tool name history: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_KeepsFinalAnswerInstruction(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "You are helpful"},
		map[string]any{"role": "user", "content": "请调用工具"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "", false)
	if !strings.Contains(finalPrompt, "请记住：唯一合法的工具调用方式是在回复末尾使用 <|ZJML|工具调用>...</|ZJML|工具调用> 代码块。") {
		t.Fatalf("finalPrompt missing final tool-call anchor instruction: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "工具调用格式") {
		t.Fatalf("finalPrompt missing xml format instruction: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "不要用 Markdown 代码围栏包裹 XML") {
		t.Fatalf("finalPrompt missing no-fence xml instruction: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, "```json") {
		t.Fatalf("finalPrompt should not require fenced tool calls: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPromptPrependsOutputIntegrityGuard(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "You are helpful"},
		map[string]any{"role": "user", "content": "请调用工具"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "", false)
	guardIdx := strings.Index(finalPrompt, "输出完整性提醒")
	toolIdx := strings.Index(finalPrompt, "工具调用格式")
	if guardIdx < 0 {
		t.Fatalf("expected output integrity guard in final prompt, got: %q", finalPrompt)
	}
	if toolIdx < 0 {
		t.Fatalf("expected tool instructions in final prompt, got: %q", finalPrompt)
	}
	if guardIdx > toolIdx {
		t.Fatalf("expected output integrity guard to precede tool instructions, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPromptReadLikeToolIncludesCacheGuard(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "请读取文件"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "read_file",
				"description": "Read a file",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "", false)
	if !strings.Contains(finalPrompt, "读取类工具缓存提示") {
		t.Fatalf("read-like tool prompt missing cache guard: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "没有给出文件正文") {
		t.Fatalf("read-like tool prompt missing no-body handling: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "不要为获取缺失正文而反复发起相同的读取请求") {
		t.Fatalf("read-like tool prompt missing loop guard: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPromptNonReadToolOmitsCacheGuard(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "搜索一下"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "Search docs",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "", false)
	if strings.Contains(finalPrompt, "读取类工具缓存提示") {
		t.Fatalf("non-read tool prompt should not include read cache guard: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPromptWithThinkingKeepsPromptUnchanged(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "继续回答上一个问题"},
	}

	finalPromptThinking, _ := buildOpenAIFinalPrompt(messages, nil, "", true)
	finalPromptPlain, _ := buildOpenAIFinalPrompt(messages, nil, "", false)
	if finalPromptThinking != finalPromptPlain {
		t.Fatalf("expected thinking flag not to prepend continuation contract, thinking=%q plain=%q", finalPromptThinking, finalPromptPlain)
	}
}
