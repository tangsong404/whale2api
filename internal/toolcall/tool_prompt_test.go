package toolcall

import (
	"strings"
	"testing"
)

func TestBuildToolCallInstructions_ExecCommandUsesCmdExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"exec_command"})
	if !strings.Contains(out, `<|ZJML|调用项 name="exec_command">`) {
		t.Fatalf("expected exec_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<|ZJML|形参 name="cmd"><![CDATA[pwd]]></|ZJML|形参>`) {
		t.Fatalf("expected cmd parameter example for exec_command, got: %s", out)
	}
}

func TestBuildToolCallInstructions_ExecuteCommandUsesCommandExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"execute_command"})
	if !strings.Contains(out, `<|ZJML|调用项 name="execute_command">`) {
		t.Fatalf("expected execute_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<|ZJML|形参 name="command"><![CDATA[pwd]]></|ZJML|形参>`) {
		t.Fatalf("expected command parameter example for execute_command, got: %s", out)
	}
}

func TestBuildToolCallInstructions_BashUsesCommandAndDescriptionExamples(t *testing.T) {
	out := BuildToolCallInstructions([]string{"Bash"})
	blocks := findInvokeBlocks(out, "Bash")
	if len(blocks) == 0 {
		t.Fatalf("expected Bash examples, got: %s", out)
	}

	sawDescription := false
	for _, block := range blocks {
		if !strings.Contains(block, `<|ZJML|形参 name="command">`) {
			t.Fatalf("expected every Bash example to use command parameter, got: %s", block)
		}
		if strings.Contains(block, `<|ZJML|形参 name="path">`) || strings.Contains(block, `<|ZJML|形参 name="content">`) {
			t.Fatalf("expected Bash examples not to use file write parameters, got: %s", block)
		}
		if strings.Contains(block, `<|ZJML|形参 name="description">`) {
			sawDescription = true
		}
	}
	if !sawDescription {
		t.Fatalf("expected Bash long-script example to include description, got: %s", out)
	}
	if strings.Contains(out, `<|ZJML|调用项 name="Read">`) {
		t.Fatalf("expected examples to avoid unavailable hard-coded Read tool, got: %s", out)
	}
}

func TestBuildToolCallInstructions_ExecuteCommandLongScriptUsesCommand(t *testing.T) {
	out := BuildToolCallInstructions([]string{"execute_command"})
	blocks := findInvokeBlocks(out, "execute_command")
	if len(blocks) == 0 {
		t.Fatalf("expected execute_command examples, got: %s", out)
	}

	for _, block := range blocks {
		if !strings.Contains(block, `<|ZJML|形参 name="command">`) {
			t.Fatalf("expected execute_command examples to use command parameter, got: %s", block)
		}
		if strings.Contains(block, `<|ZJML|形参 name="path">`) || strings.Contains(block, `<|ZJML|形参 name="content">`) {
			t.Fatalf("expected execute_command examples not to use file write parameters, got: %s", block)
		}
	}
	if !strings.Contains(out, `test_escape.sh`) {
		t.Fatalf("expected execute_command long-script example, got: %s", out)
	}
}

func TestBuildToolCallInstructions_ExecCommandLongScriptUsesCmd(t *testing.T) {
	out := BuildToolCallInstructions([]string{"exec_command"})
	blocks := findInvokeBlocks(out, "exec_command")
	if len(blocks) == 0 {
		t.Fatalf("expected exec_command examples, got: %s", out)
	}

	for _, block := range blocks {
		if !strings.Contains(block, `<|ZJML|形参 name="cmd">`) {
			t.Fatalf("expected exec_command examples to use cmd parameter, got: %s", block)
		}
		if strings.Contains(block, `<|ZJML|形参 name="command">`) || strings.Contains(block, `<|ZJML|形参 name="path">`) || strings.Contains(block, `<|ZJML|形参 name="content">`) {
			t.Fatalf("expected exec_command examples not to use command or file write parameters, got: %s", block)
		}
	}
	if !strings.Contains(out, `test_escape.sh`) {
		t.Fatalf("expected exec_command long-script example, got: %s", out)
	}
}

func TestBuildToolCallInstructions_WriteUsesFilePathAndContent(t *testing.T) {
	out := BuildToolCallInstructions([]string{"Write"})
	blocks := findInvokeBlocks(out, "Write")
	if len(blocks) == 0 {
		t.Fatalf("expected Write examples, got: %s", out)
	}

	for _, block := range blocks {
		if !strings.Contains(block, `<|ZJML|形参 name="file_path">`) || !strings.Contains(block, `<|ZJML|形参 name="content">`) {
			t.Fatalf("expected Write examples to use file_path and content, got: %s", block)
		}
		if strings.Contains(block, `<|ZJML|形参 name="path">`) {
			t.Fatalf("expected Write examples not to use path, got: %s", block)
		}
	}
}

func TestBuildToolCallInstructions_AnchorsMissingOpeningWrapperFailureMode(t *testing.T) {
	out := BuildToolCallInstructions([]string{"read_file"})
	if !strings.Contains(out, "禁止省略开头的 <|ZJML|工具调用> 标签") {
		t.Fatalf("expected explicit missing-opening-tag warning, got: %s", out)
	}
	if !strings.Contains(out, "错误 3 — 缺少开头包裹") {
		t.Fatalf("expected missing-opening-wrapper negative example, got: %s", out)
	}
}

func findInvokeBlocks(text, name string) []string {
	open := `<|ZJML|调用项 name="` + name + `">`
	remaining := text
	blocks := []string{}
	for {
		start := strings.Index(remaining, open)
		if start < 0 {
			return blocks
		}
		remaining = remaining[start:]
		end := strings.Index(remaining, `</|ZJML|调用项>`)
		if end < 0 {
			return blocks
		}
		end += len(`</|ZJML|调用项>`)
		blocks = append(blocks, remaining[:end])
		remaining = remaining[end:]
	}
}
