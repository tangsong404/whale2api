package openai

import "testing"

func TestBuildResponsesUsageForModel(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		finalPrompt   string
		finalThinking string
		finalText     string
		refFileTokens int
		wantInput     int
		wantOutput    int
		wantTotal     int
	}{
		{
			name:        "all empty",
			finalPrompt: "",
			wantInput:   0,
			wantOutput:  0,
			wantTotal:   0,
		},
		{
			name:        "prompt only",
			finalPrompt: "hello",
			wantInput:   5,
			wantOutput:  0,
			wantTotal:   5,
		},
		{
			name:        "prompt and text no thinking",
			finalPrompt: "hello world",
			finalText:   "response",
			wantInput:   6,
			wantOutput:  2,
			wantTotal:   8,
		},
		{
			name:          "prompt thinking and text",
			finalPrompt:   "hello world",
			finalThinking: "thinking here",
			finalText:     "response",
			wantInput:     6,
			wantOutput:    5,
			wantTotal:     11,
		},
		{
			name:          "ref file tokens added to input",
			finalPrompt:   "hello",
			finalText:     "hi",
			refFileTokens: 10,
			wantInput:     15,
			wantOutput:    1,
			wantTotal:     16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := BuildResponsesUsageForModel(tt.model, tt.finalPrompt, tt.finalThinking, tt.finalText, tt.refFileTokens)
			if got, _ := usage["input_tokens"].(int); got != tt.wantInput {
				t.Errorf("input_tokens = %d, want %d", got, tt.wantInput)
			}
			if got, _ := usage["output_tokens"].(int); got != tt.wantOutput {
				t.Errorf("output_tokens = %d, want %d", got, tt.wantOutput)
			}
			if got, _ := usage["total_tokens"].(int); got != tt.wantTotal {
				t.Errorf("total_tokens = %d, want %d", got, tt.wantTotal)
			}
		})
	}
}

func TestBuildResponsesUsage(t *testing.T) {
	usage := BuildResponsesUsage("hello world", "thinking here", "response")
	if got, _ := usage["input_tokens"].(int); got != 6 {
		t.Errorf("input_tokens = %d, want 6", got)
	}
	if got, _ := usage["output_tokens"].(int); got != 5 {
		t.Errorf("output_tokens = %d, want 5", got)
	}
	if got, _ := usage["total_tokens"].(int); got != 11 {
		t.Errorf("total_tokens = %d, want 11", got)
	}
}
