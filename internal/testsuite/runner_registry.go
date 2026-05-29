package testsuite

import "context"

type caseDef struct {
	ID  string
	Run func(context.Context, *caseContext) error
}

func (r *Runner) cases() []caseDef {
	return []caseDef{
		{ID: "healthz_ok", Run: r.caseHealthz},
		{ID: "readyz_ok", Run: r.caseReadyz},
		{ID: "models_openai", Run: r.caseModelsOpenAI},
		{ID: "model_openai_by_id", Run: r.caseModelOpenAIByID},
		{ID: "chat_nonstream_basic", Run: r.caseChatNonstream},
		{ID: "chat_stream_basic", Run: r.caseChatStream},
		{ID: "responses_nonstream_basic", Run: r.caseResponsesNonstream},
		{ID: "responses_stream_basic", Run: r.caseResponsesStream},
		{ID: "embeddings_contract", Run: r.caseEmbeddings},
		{ID: "reasoner_stream", Run: r.caseReasonerStream},
		{ID: "toolcall_nonstream", Run: r.caseToolcallNonstream},
		{ID: "toolcall_stream", Run: r.caseToolcallStream},
		{ID: "concurrency_burst", Run: r.caseConcurrencyBurst},
		{ID: "toolcall_stream_mixed", Run: r.caseToolcallStreamMixed},
		{ID: "sse_json_integrity", Run: r.caseSSEJSONIntegrity},
		{ID: "error_contract_invalid_model", Run: r.caseInvalidModel},
		{ID: "error_contract_missing_messages", Run: r.caseMissingMessages},
		{ID: "error_contract_invalid_key", Run: r.caseInvalidKey},
	}
}
