package shared

import (
	"strings"

	"whale2api/internal/sse"
)

type StreamAccumulator struct {
	ThinkingEnabled       bool
	SearchEnabled         bool
	StripReferenceMarkers bool

	RawThinking           strings.Builder
	Thinking              strings.Builder
	ToolDetectionThinking strings.Builder
	RawText               strings.Builder
	Text                  strings.Builder
}

type StreamPartDelta struct {
	Type         string
	RawText      string
	VisibleText  string
	CitationOnly bool
}

type StreamAccumulatorResult struct {
	ContentSeen bool
	Parts       []StreamPartDelta
}

func (a *StreamAccumulator) Apply(parsed sse.LineResult) StreamAccumulatorResult {
	out := StreamAccumulatorResult{}
	for _, p := range parsed.ToolDetectionThinkingParts {
		trimmed := sse.TrimContinuationOverlapFromBuilder(&a.ToolDetectionThinking, p.Text)
		if trimmed != "" {
			a.ToolDetectionThinking.WriteString(trimmed)
		}
	}
	for _, p := range parsed.Parts {
		if p.Type == "thinking" {
			delta := a.applyThinkingPart(p.Text)
			if delta.RawText != "" {
				out.ContentSeen = true
			}
			if delta.RawText != "" || delta.VisibleText != "" {
				out.Parts = append(out.Parts, delta)
			}
			continue
		}
		delta := a.applyTextPart(p.Text)
		if delta.RawText != "" {
			out.ContentSeen = true
		}
		if delta.RawText != "" || delta.VisibleText != "" || delta.CitationOnly {
			out.Parts = append(out.Parts, delta)
		}
	}
	return out
}

func (a *StreamAccumulator) applyThinkingPart(text string) StreamPartDelta {
	rawTrimmed := sse.TrimContinuationOverlapFromBuilder(&a.RawThinking, text)
	if rawTrimmed != "" {
		a.RawThinking.WriteString(rawTrimmed)
	}
	delta := StreamPartDelta{Type: "thinking", RawText: rawTrimmed}
	if !a.ThinkingEnabled || rawTrimmed == "" {
		return delta
	}
	cleanedText := CleanVisibleOutput(rawTrimmed, a.StripReferenceMarkers)
	if cleanedText == "" {
		return delta
	}
	trimmed := sse.TrimContinuationOverlapFromBuilder(&a.Thinking, cleanedText)
	if trimmed == "" {
		return delta
	}
	a.Thinking.WriteString(trimmed)
	delta.VisibleText = trimmed
	return delta
}

func (a *StreamAccumulator) applyTextPart(text string) StreamPartDelta {
	rawTrimmed := sse.TrimContinuationOverlapFromBuilder(&a.RawText, text)
	if rawTrimmed == "" {
		return StreamPartDelta{Type: "text"}
	}
	a.RawText.WriteString(rawTrimmed)
	delta := StreamPartDelta{Type: "text", RawText: rawTrimmed}
	if a.SearchEnabled && sse.IsCitation(rawTrimmed) {
		delta.CitationOnly = true
		return delta
	}
	cleanedText := CleanVisibleOutput(rawTrimmed, a.StripReferenceMarkers)
	trimmed := sse.TrimContinuationOverlapFromBuilder(&a.Text, cleanedText)
	if trimmed == "" {
		return delta
	}
	a.Text.WriteString(trimmed)
	delta.VisibleText = trimmed
	return delta
}
