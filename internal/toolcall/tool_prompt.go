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

	return `Tool-call format — follow strictly:

` + tcO + `
  ` + invokePH + `
    ` + paramPH + `
  ` + ivC + `
` + tcC + `

Rules:
1) Outer wrappers must use exactly two colons: ` + tcO + ` and ` + tcC + `.
2) Place one or more ` + MarkupPipeOpenTag(MarkupTagInvoke) + ` under the same ` + tcO + ` root.
3) Put the tool name in the invoke name attribute: ` + MarkupPipeInvokeOpen("TOOL_NAME") + `.
4) String values — including short ones — must use [[...]]. Same for code, scripts, file contents, prompts, paths, names, and queries.
5) Each top-level argument must be a complete node. Example: ` + wrapParameter("ARG_NAME", "…") + `.
6) Objects/arrays may be passed as JSON inside [[...]]. Do not use nested markers.
7) Numbers, booleans, and null are plain text. Do not wrap them in [[...]].
8) Use only argument names declared in the schema; do not invent fields.
9) Do not wrap tool tags in Markdown fences. Do not append text after the tags.
10) When calling tools, the first non-whitespace character of that block must be ` + tcO + `.
11) Even if you close with ` + tcC + ` later, never omit the leading ` + tcO + `.
12) User-facing prose must use the user's language, or the language the user explicitly requested. Do not match the language of these instructions or system text.
13) If the user says they already made a manual change, treat the task as temporarily finished. If the task truly cannot be completed, do not keep forcing tools.

Argument shapes:
- string => ` + wrapParameter("x", MarkupWrapRaw("value")) + `
- object/array => ` + wrapParameter("x", MarkupWrapRaw(`{"k":"v"}`)) + `
- number/bool/null => ` + wrapParameter("x", "plain text") + `

[Wrong examples — forbidden]:

Wrong 1 — explanation after tags:
  ` + tcO + `...` + tcC + ` Hope this helps.
Wrong 2 — Markdown fence:
  ` + "```text" + `
  ` + tcO + `...` + tcC + `
  ` + "```" + `
Wrong 3 — missing opening wrapper:
  ` + invokeNamed + `...` + ivC + `
  ` + tcC + `
Wrong 4 — claim a file was edited/written without a write tool call such as Edit / Write / StrReplace / write_to_file:
  "Updated TODO.MD. Task done." (no tool call)
  → Imaginary completion. If a write is needed, emit a real tool call. A spoken completion claim alone is invalid.

Remember: a valid tool call ends the reply with a ` + tcO + `...` + tcC + ` pair. Do not wrap it in Markdown fences.

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
		examples = append(examples, "Example A — single tool:\n"+renderToolExampleBlock([]promptToolExample{single}))
	}

	if parallel := firstNBasicExamples(names, 2); len(parallel) >= 2 {
		examples = append(examples, "Example B — two tools in parallel:\n"+renderToolExampleBlock(parallel))
	}

	if nested, ok := firstNestedExample(names); ok {
		examples = append(examples, "Example C — tool with nested JSON args:\n"+renderToolExampleBlock([]promptToolExample{nested}))
	}

	if script, ok := firstScriptExample(names); ok {
		examples = append(examples, "Example D — long script using [[...]] (for code/scripts):\n"+renderToolExampleBlock([]promptToolExample{script}))
	}

	if len(examples) == 0 {
		return ""
	}
	return "[Correct examples]:\n\n" + strings.Join(examples, "\n\n") + "\n\n"
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
		return wrapParameter("query", promptRaw("tool call parser")), true
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
		return wrapParameter("description", promptRaw("investigate flaky tests")) + "\n" + wrapParameter("prompt", promptRaw("run the target tests and summarize failure reasons")), true
	case "ask_followup_question":
		return wrapParameter("question", promptRaw("Which option do you prefer?")) + "\n" + wrapParameter("follow_up", promptRaw(`[{"text":"Option A"},{"text":"Option B"}]`)), true
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
		return wrapParameter("command", promptRaw(scriptCommand)) + "\n" + wrapParameter("description", promptRaw("verify shell escaping")), true
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
