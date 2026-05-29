package shared

import (
	"whale2api/internal/config"
	"whale2api/internal/promptcompat"
)

// ThinkingInjectionConfig is the subset of config needed to rewrite prompts for thinking models.
type ThinkingInjectionConfig interface {
	ThinkingInjectionEnabled() bool
	ThinkingInjectionPrompt() string
}

func ApplyThinkingInjection(store ThinkingInjectionConfig, stdReq promptcompat.StandardRequest) promptcompat.StandardRequest {
	if store == nil {
		config.Logger.Debug("[thinking_injection] skip: nil store")
		return stdReq
	}
	if !store.ThinkingInjectionEnabled() {
		config.Logger.Debug("[thinking_injection] skip: disabled (config/env); upstream thinking_enabled unchanged", "std_thinking", stdReq.Thinking)
		return stdReq
	}
	if !stdReq.Thinking {
		config.Logger.Debug("[thinking_injection] skip: stdReq.Thinking false")
		return stdReq
	}
	messages, changed := promptcompat.AppendThinkingInjectionPromptToLatestUser(stdReq.Messages, store.ThinkingInjectionPrompt())
	if !changed {
		config.Logger.Debug("[thinking_injection] skip: duplicate marker or no user message")
		return stdReq
	}
	finalPrompt, toolNames := promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	if len(toolNames) == 0 && len(stdReq.ToolNames) > 0 {
		toolNames = stdReq.ToolNames
	}
	stdReq.Messages = messages
	stdReq.FinalPrompt = finalPrompt
	stdReq.ToolNames = toolNames
	config.Logger.Debug("[thinking_injection] applied: appended prompt to latest user")
	return stdReq
}
