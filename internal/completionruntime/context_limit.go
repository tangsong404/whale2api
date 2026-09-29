package completionruntime

import (
	"fmt"
	"net/http"
	"strings"

	"whale2api/internal/assistantturn"
	"whale2api/internal/config"
	"whale2api/internal/promptcompat"
	"whale2api/internal/util"
)

// userFacingContextLimitTokens is the limit described to clients in error
// text. Default is 896K; WHALE2API_USER_FACING_CONTEXT_LIMIT_TOKENS can change it.
func userFacingContextLimitTokens() int {
	return config.EffectiveUserFacingContextLimitTokens
}

// logicalContextLimitTokens is the internal tokenizer gate. Requests above it
// are rejected before DeepSeek. Default is 2.85M: the local estimate measured
// at the ~890K-token stable upstream boundary. It can be changed with
// WHALE2API_LOGICAL_CONTEXT_LIMIT_TOKENS.
func logicalContextLimitTokens() int {
	return config.EffectiveLogicalContextLimitTokens
}

func promptTextForContextGate(stdReq promptcompat.StandardRequest) string {
	prompt := strings.TrimSpace(stdReq.PromptTokenText)
	if prompt == "" {
		return stdReq.FinalPrompt
	}
	return prompt
}

func estimatedUserInputTokens(stdReq promptcompat.StandardRequest) int {
	model := strings.TrimSpace(stdReq.ResolvedModel)
	if model == "" {
		model = strings.TrimSpace(stdReq.RequestedModel)
	}
	return util.CountPromptTokens(promptTextForContextGate(stdReq), model) + stdReq.RefFileTokens
}

func userFacingTokenEstimate(internalEstimated int) int {
	if internalEstimated <= 0 {
		return 0
	}
	logical := logicalContextLimitTokens()
	if logical <= 0 {
		return 0
	}
	// Map internal tokenizer count to the user-facing limit space.
	return (internalEstimated*userFacingContextLimitTokens() + logical/2) / logical
}

// requestedCompletionTokens mirrors the official DeepSeek API error, which
// counts the client's max_tokens / max_completion_tokens as the completion
// budget. Missing/invalid values count as 0.
func requestedCompletionTokens(stdReq promptcompat.StandardRequest) int {
	for _, key := range []string{"max_completion_tokens", "max_tokens"} {
		v, ok := stdReq.PassThrough[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			if n > 0 {
				return int(n)
			}
		case int:
			if n > 0 {
				return n
			}
		case int64:
			if n > 0 {
				return int(n)
			}
		}
	}
	return 0
}

// contextLengthExceededMessage matches the official DeepSeek 400 message
// shape, including the messages/completion breakdown.
func contextLengthExceededMessage(stdReq promptcompat.StandardRequest, internalEstimated int) string {
	messages := userFacingTokenEstimate(internalEstimated)
	completion := requestedCompletionTokens(stdReq)
	return fmt.Sprintf(
		"This model's maximum context length is %d tokens. However, you requested %d tokens (%d in the messages, %d in the completion). Please reduce the length of the messages or completion.",
		userFacingContextLimitTokens(),
		messages+completion,
		messages,
		completion,
	)
}

func userContextOverGate(stdReq promptcompat.StandardRequest) *assistantturn.OutputError {
	estimated := estimatedUserInputTokens(stdReq)
	if estimated <= logicalContextLimitTokens() {
		return nil
	}
	return &assistantturn.OutputError{
		Status:  http.StatusBadRequest,
		Message: contextLengthExceededMessage(stdReq, estimated),
		Code:    "invalid_request_error",
		Param:   "",
	}
}
