package promptcompat

import "testing"

func TestStandardRequestCompletionPayloadSetsModelTypeFromResolvedModel(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		thinking  bool
		search    bool
		modelType string
	}{
		{name: "default", model: "deepseek-v4-flash", thinking: false, search: false, modelType: "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := StandardRequest{
				ResolvedModel: tc.model,
				FinalPrompt:   "hello",
				Thinking:      tc.thinking,
				Search:        tc.search,
				RefFileIDs:    []string{"file-a", "file-b"},
				PassThrough: map[string]any{
					"temperature": 0.3,
				},
			}

			payload := req.CompletionPayload("session-123")

			if got := payload["model_type"]; got != tc.modelType {
				t.Fatalf("expected model_type %s, got %#v", tc.modelType, got)
			}
			if got := payload["chat_session_id"]; got != "session-123" {
				t.Fatalf("unexpected chat_session_id: %#v", got)
			}
			if got := payload["thinking_enabled"]; got != tc.thinking {
				t.Fatalf("unexpected thinking_enabled: %#v", got)
			}
			if got := payload["search_enabled"]; got != tc.search {
				t.Fatalf("unexpected search_enabled: %#v", got)
			}
			if got := payload["temperature"]; got != 0.3 {
				t.Fatalf("expected passthrough temperature, got %#v", got)
			}
			refFileIDs, ok := payload["ref_file_ids"].([]any)
			if !ok {
				t.Fatalf("expected ref_file_ids slice, got %#v", payload["ref_file_ids"])
			}
			if len(refFileIDs) != 2 || refFileIDs[0] != "file-a" || refFileIDs[1] != "file-b" {
				t.Fatalf("unexpected ref_file_ids: %#v", refFileIDs)
			}
		})
	}
}

func TestStandardRequestCompletionPayloadPassThroughCannotForceExpert(t *testing.T) {
	req := StandardRequest{
		ResolvedModel: "deepseek-v4-flash",
		FinalPrompt:   "hello",
		Thinking:      true,
		Search:        false,
		PassThrough: map[string]any{
			"model_type": "expert",
		},
	}
	payload := req.CompletionPayload("session-1")
	if got := payload["model_type"]; got != "default" {
		t.Fatalf("expected model_type default, got %#v", got)
	}
}

func TestStandardRequestCompletionPayloadPassThroughDeepseekProString(t *testing.T) {
	req := StandardRequest{
		ResolvedModel: "deepseek-v4-flash",
		FinalPrompt:   "hello",
		PassThrough: map[string]any{
			"model_type": "deepseek-v4-pro",
		},
	}
	payload := req.CompletionPayload("session-1")
	if got := payload["model_type"]; got != "default" {
		t.Fatalf("expected model_type default, got %#v", got)
	}
}
