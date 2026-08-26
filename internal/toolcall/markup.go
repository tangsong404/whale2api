package toolcall

import "strings"

// Colon-style tool markup emitted in prompts and preferred in model output.
// Legacy <|ZJML|…> / <|DSML|…> / <tool_calls> remain accepted by the scanner.
const (
	MarkupTagToolCalls = "tc"
	MarkupTagInvoke    = "invoke"
	MarkupTagParameter = "param"
)

// Deprecated: kept so older call sites compile; emission no longer uses a pipe channel.
const MarkupPipeChannel = ""

func MarkupPipeOpenTag(localName string) string {
	return "::" + localName + "::"
}

func MarkupPipeCloseTag(localName string) string {
	return "::/" + localName + "::"
}

// MarkupPipeInvokeOpen renders ::invoke name="toolName"::.
func MarkupPipeInvokeOpen(toolName string) string {
	return "::" + MarkupTagInvoke + ` name="` + toolName + `"::`
}

// MarkupParamOpen renders ::param name="paramName"::.
func MarkupParamOpen(paramName string) string {
	return "::" + MarkupTagParameter + ` name="` + paramName + `"::`
}

// MarkupRawOpen/Close wrap opaque string values without angle brackets.
func MarkupRawOpen() string  { return "[[" }
func MarkupRawClose() string { return "]]" }

func MarkupWrapRaw(text string) string {
	if text == "" {
		return MarkupRawOpen() + MarkupRawClose()
	}
	// Escape accidental closers inside the payload.
	escaped := strings.ReplaceAll(text, "]]", "]] ]]")
	return MarkupRawOpen() + escaped + MarkupRawClose()
}
