package completionruntime

import (
	"fmt"
	"net/http"
	"strings"

	"whale2api/internal/assistantturn"
	"whale2api/internal/promptcompat"
	"whale2api/internal/util"
)

// UserFacingContextLimitTokens is the limit described to clients in error text (256K).
const UserFacingContextLimitTokens = 256_000

// LogicalContextLimitTokens is the internal tokenizer gate (~2.88× vs upstream real context).
const LogicalContextLimitTokens = 750_000

// userContextInputGateTokens rejects above this (exclusive) before DeepSeek; errors cite 256K.
const userContextInputGateTokens = LogicalContextLimitTokens

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
	// Map internal tokenizer count to the 256K space clients expect.
	return (internalEstimated*UserFacingContextLimitTokens + LogicalContextLimitTokens/2) / LogicalContextLimitTokens
}

func contextLengthExceededMessage(internalEstimated int) string {
	return fmt.Sprintf(
		"This model's maximum context length is %d tokens. However, your messages resulted in %d tokens. Please reduce the length of the messages.",
		UserFacingContextLimitTokens,
		userFacingTokenEstimate(internalEstimated),
	)
}

func userContextOverGate(stdReq promptcompat.StandardRequest) *assistantturn.OutputError {
	estimated := estimatedUserInputTokens(stdReq)
	if estimated <= userContextInputGateTokens {
		return nil
	}
	return &assistantturn.OutputError{
		Status:  http.StatusBadRequest,
		Message: contextLengthExceededMessage(estimated),
		Code:    "context_length_exceeded",
		Param:   "messages",
	}
}
