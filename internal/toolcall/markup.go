package toolcall

// Pipe-style tool markup (emitted in prompts and parsed from model output).
// Legacy <|DSML|tool_calls> / <tool_calls> remain accepted by the scanner.
const (
	MarkupPipeChannel = "ZJML"

	MarkupTagToolCalls = "工具调用"
	MarkupTagInvoke    = "调用项"
	MarkupTagParameter = "形参"
)

func MarkupPipeOpenTag(localName string) string {
	return "<|" + MarkupPipeChannel + "|" + localName + ">"
}

func MarkupPipeCloseTag(localName string) string {
	return "</|" + MarkupPipeChannel + "|" + localName + ">"
}

// MarkupPipeInvokeOpen renders <|ZJML|调用项 name="toolName">.
func MarkupPipeInvokeOpen(toolName string) string {
	return "<|" + MarkupPipeChannel + "|" + MarkupTagInvoke + ` name="` + toolName + `">`
}
