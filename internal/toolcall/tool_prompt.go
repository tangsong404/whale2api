package toolcall

import "strings"

// BuildToolCallInstructions generates the unified tool-calling instruction block
// used by all adapters (OpenAI, Claude, Gemini). It uses attention-optimized
// structure: rules → negative examples → positive examples → anchor.
//
// The toolNames slice should contain the actual tool names available in the
// current request; the function picks real names for examples.
func BuildToolCallInstructions(toolNames []string) string {
	tcO := MarkupPipeOpenTag(MarkupTagToolCalls)
	tcC := MarkupPipeCloseTag(MarkupTagToolCalls)
	ivC := MarkupPipeCloseTag(MarkupTagInvoke)
	paramPH := wrapParameter("PARAMETER_NAME", MarkupWrapRaw("PARAMETER_VALUE"))
	invokePH := MarkupPipeInvokeOpen("TOOL_NAME_HERE")
	invokeNamed := MarkupPipeInvokeOpen("TOOL_NAME")

	return `ツール呼び出し形式 — 厳守すること：

` + tcO + `
  ` + invokePH + `
    ` + paramPH + `
  ` + ivC + `
` + tcC + `

規則：
1）外側の包みはコロンちょうど2つで書く：` + tcO + ` と ` + tcC + `。
2）同一の ` + tcO + ` ルートの下に、1つ以上の ` + MarkupPipeOpenTag(MarkupTagInvoke) + ` を置くこと。
3）ツール名は呼び出し項の name 属性に書く：` + MarkupPipeInvokeOpen("TOOL_NAME") + `。
4）文字列値は短いものも含め、必ず [[...]] を使う。コード、スクリプト、ファイル内容、プロンプト、パス、名前、クエリも同様。
5）各トップレベル引数は完全なノードにすること。例：` + wrapParameter("ARG_NAME", "…") + `。
6）オブジェクト・配列は [[...]] 内の JSON で渡してよい。入れ子マーカーは使わないこと。
7）数値・真偽値・null はプレーンテキスト。[[...]] は使わない。
8）スキーマで宣言された引数名だけを使い、勝手にフィールドを作らないこと。
9）Markdown のコードフェンスでツールタグを包まないこと。タグの後に文字を足さないこと。
10）ツールを呼ぶ場合、そのブロックの最初の非空白文字は必ず ` + tcO + ` であること。
11）後で ` + tcC + ` を閉じる場合でも、先頭の ` + tcO + ` を省略してはならない。
12）ユーザー向け本文は、ユーザーの言語、またはユーザーが明示した言語で書くこと。本指示文・システム文の言語に合わせて出力してはならない。
13）ユーザーが自分で手動修正したと述べた場合は、タスクは一時終了とみなしてよい。タスクがどうしても完了できない場合も、無理に続けないこと。完了／一時終了なら『タスク完了』のみ、完了不可なら『タスク完了不可』のみを出し、同じ文言の繰り返しは絶対に禁止。

引数の形：
- 文字列 => ` + wrapParameter("x", MarkupWrapRaw("value")) + `
- オブジェクト/配列 => ` + wrapParameter("x", MarkupWrapRaw(`{"k":"v"}`)) + `
- 数値/真偽/null => ` + wrapParameter("x", "プレーンテキスト") + `

【誤った例 — 禁止】：

誤り 1 — タグの後に説明文：
  ` + tcO + `...` + tcC + ` お役に立てれば幸いです。
誤り 2 — Markdown フェンス：
  ` + "```text" + `
  ` + tcO + `...` + tcC + `
  ` + "```" + `
誤り 3 — 先頭の包みがない：
  ` + invokeNamed + `...` + ivC + `
  ` + tcC + `
誤り 4 — ファイルを編集・書き込んだと宣言したが、Edit / Write / StrReplace / write_to_file 等の書き込み系ツール呼び出しがない：
  「TODO.MD を修正しました。タスク完了です。」（ツール呼び出しなし）
  → 空想上の完了。書き込みが必要なら本物のツール呼び出しを出すこと。口頭の完了宣言だけでは不可。

覚えておくこと：合法なツール呼び出しは、返信末尾に ` + tcO + `...` + tcC + ` のタグ対を出すこと。Markdown フェンスで包まないこと。

` + buildCorrectToolExamples(toolNames)
}

type promptToolExample struct {
	name   string
	params string
}

func buildCorrectToolExamples(toolNames []string) string {
	names := uniqueToolNames(toolNames)
	examples := make([]string, 0, 4)

	if single, ok := firstBasicExample(names); ok {
		examples = append(examples, "例 A — 単一ツール：\n"+renderToolExampleBlock([]promptToolExample{single}))
	}

	if parallel := firstNBasicExamples(names, 2); len(parallel) >= 2 {
		examples = append(examples, "例 B — 2つのツールを並列：\n"+renderToolExampleBlock(parallel))
	}

	if nested, ok := firstNestedExample(names); ok {
		examples = append(examples, "例 C — JSON 入れ子引数のあるツール：\n"+renderToolExampleBlock([]promptToolExample{nested}))
	}

	if script, ok := firstScriptExample(names); ok {
		examples = append(examples, "例 D — [[...]] を使う長いスクリプト（コード/スクリプト向け）：\n"+renderToolExampleBlock([]promptToolExample{script}))
	}

	if len(examples) == 0 {
		return ""
	}
	return "【正しい例】：\n\n" + strings.Join(examples, "\n\n") + "\n\n"
}

func uniqueToolNames(toolNames []string) []string {
	names := make([]string, 0, len(toolNames))
	seen := map[string]bool{}
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func firstBasicExample(names []string) (promptToolExample, bool) {
	for _, name := range names {
		if params, ok := exampleBasicParams(name); ok {
			return promptToolExample{name: name, params: params}, true
		}
	}
	return promptToolExample{}, false
}

func firstNBasicExamples(names []string, count int) []promptToolExample {
	out := make([]promptToolExample, 0, count)
	for _, name := range names {
		if params, ok := exampleBasicParams(name); ok {
			out = append(out, promptToolExample{name: name, params: params})
			if len(out) == count {
				return out
			}
		}
	}
	return out
}

func firstNestedExample(names []string) (promptToolExample, bool) {
	for _, name := range names {
		if params, ok := exampleNestedParams(name); ok {
			return promptToolExample{name: name, params: params}, true
		}
	}
	return promptToolExample{}, false
}

func firstScriptExample(names []string) (promptToolExample, bool) {
	for _, name := range names {
		if params, ok := exampleScriptParams(name); ok {
			return promptToolExample{name: name, params: params}, true
		}
	}
	return promptToolExample{}, false
}

func renderToolExampleBlock(calls []promptToolExample) string {
	var b strings.Builder
	b.WriteString(MarkupPipeOpenTag(MarkupTagToolCalls) + "\n")
	for _, call := range calls {
		b.WriteString("  " + MarkupPipeInvokeOpen(call.name) + "\n")
		b.WriteString(indentPromptParameters(call.params, "    "))
		b.WriteString("\n  " + MarkupPipeCloseTag(MarkupTagInvoke) + "\n")
	}
	b.WriteString(MarkupPipeCloseTag(MarkupTagToolCalls))
	return b.String()
}

func indentPromptParameters(body, indent string) string {
	if strings.TrimSpace(body) == "" {
		return indent + MarkupParamOpen("content") + MarkupPipeCloseTag(MarkupTagParameter)
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = line
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

func wrapParameter(name, inner string) string {
	return MarkupParamOpen(name) + inner + MarkupPipeCloseTag(MarkupTagParameter)
}

func exampleBasicParams(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "Read":
		return wrapParameter("file_path", promptRaw("README.md")), true
	case "Glob":
		return wrapParameter("pattern", promptRaw("**/*.go")) + "\n" + wrapParameter("path", promptRaw(".")), true
	case "read_file":
		return wrapParameter("path", promptRaw("src/main.go")), true
	case "list_files":
		return wrapParameter("path", promptRaw(".")), true
	case "search_files":
		return wrapParameter("query", promptRaw("ツール呼び出しパーサー")), true
	case "Bash", "execute_command":
		return wrapParameter("command", promptRaw("pwd")), true
	case "exec_command":
		return wrapParameter("cmd", promptRaw("pwd")), true
	case "Write":
		return wrapParameter("file_path", promptRaw("notes.txt")) + "\n" + wrapParameter("content", promptRaw("Hello world")), true
	case "write_to_file":
		return wrapParameter("path", promptRaw("notes.txt")) + "\n" + wrapParameter("content", promptRaw("Hello world")), true
	case "Edit":
		return wrapParameter("file_path", promptRaw("README.md")) + "\n" + wrapParameter("old_string", promptRaw("foo")) + "\n" + wrapParameter("new_string", promptRaw("bar")), true
	case "MultiEdit":
		return wrapParameter("file_path", promptRaw("README.md")) + "\n" + wrapParameter("edits", promptRaw(`[{"old_string":"foo","new_string":"bar"}]`)), true
	}
	return "", false
}

func exampleNestedParams(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "MultiEdit":
		return wrapParameter("file_path", promptRaw("README.md")) + "\n" + wrapParameter("edits", promptRaw(`[{"old_string":"foo","new_string":"bar"}]`)), true
	case "Task":
		return wrapParameter("description", promptRaw("不安定なテストを調査")) + "\n" + wrapParameter("prompt", promptRaw("対象テストを実行し失敗理由をまとめる")), true
	case "ask_followup_question":
		return wrapParameter("question", promptRaw("どの案がよいですか？")) + "\n" + wrapParameter("follow_up", promptRaw(`[{"text":"案 A"},{"text":"案 B"}]`)), true
	}
	return "", false
}

func exampleScriptParams(name string) (string, bool) {
	scriptCommand := `cat > /tmp/test_escape.sh <<'EOF'
#!/bin/bash
echo 'single "double"'
echo "literal dollar: \$HOME"
EOF
bash /tmp/test_escape.sh`
	scriptContent := `#!/bin/bash
echo 'single "double"'
echo "literal dollar: $HOME"`

	switch strings.TrimSpace(name) {
	case "Bash":
		return wrapParameter("command", promptRaw(scriptCommand)) + "\n" + wrapParameter("description", promptRaw("シェルエスケープの確認")), true
	case "execute_command":
		return wrapParameter("command", promptRaw(scriptCommand)), true
	case "exec_command":
		return wrapParameter("cmd", promptRaw(scriptCommand)), true
	case "Write":
		return wrapParameter("file_path", promptRaw("test_escape.sh")) + "\n" + wrapParameter("content", promptRaw(scriptContent)), true
	case "write_to_file":
		return wrapParameter("path", promptRaw("test_escape.sh")) + "\n" + wrapParameter("content", promptRaw(scriptContent)), true
	}
	return "", false
}

func promptRaw(text string) string {
	return MarkupWrapRaw(text)
}
