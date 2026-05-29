package sse

import (
	"fmt"
)

// LineResult is the normalized parse result for one DeepSeek SSE line.
type LineResult struct {
	Parsed                     bool
	Stop                       bool
	ContentFilter              bool
	ErrorMessage               string
	Parts                      []ContentPart
	ToolDetectionThinkingParts []ContentPart
	NextType                   string
	ResponseMessageID          int
}

// ParseDeepSeekContentLine centralizes one-line DeepSeek SSE parsing for both
// streaming and non-streaming handlers.
func ParseDeepSeekContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed {
		return LineResult{NextType: currentType}
	}
	if done {
		return LineResult{Parsed: true, Stop: true, NextType: currentType}
	}
	if errObj, hasErr := chunk["error"]; hasErr {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: fmt.Sprintf("%v", errObj),
			NextType:     currentType,
		}
	}
	if code, _ := chunk["code"].(string); code == "content_filter" {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			NextType:      currentType,
		}
	}
	if hasContentFilterStatus(chunk) {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			NextType:      currentType,
		}
	}
	parts, detectionThinkingParts, finished, nextType := ParseSSEChunkForContentDetailed(chunk, thinkingEnabled, currentType)
	parts = filterLeakedContentFilterParts(parts)
	detectionThinkingParts = filterLeakedContentFilterParts(detectionThinkingParts)
	var respMsgID int
	observeResponseMessageID(chunk, &respMsgID)
	return LineResult{
		Parsed:                     true,
		Stop:                       finished,
		Parts:                      parts,
		ToolDetectionThinkingParts: detectionThinkingParts,
		NextType:                   nextType,
		ResponseMessageID:          respMsgID,
	}
}
