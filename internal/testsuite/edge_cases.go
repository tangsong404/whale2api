package testsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (r *Runner) caseToolcallStreamMixed(ctx context.Context, cc *caseContext) error {
	payload := toolcallPayload(true)
	payload["messages"] = []map[string]any{
		{
			"role":    "user",
			"content": "请先输出一句普通文本，再调用工具 search 查询 golang，最后再输出一句普通文本。",
		},
	}
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      payload,
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasTool := false
	hasText := false
	rawLeak := false
	for _, f := range frames {
		choices, _ := f["choices"].([]any)
		for _, c := range choices {
			ch, _ := c.(map[string]any)
			delta, _ := ch["delta"].(map[string]any)
			if _, ok := delta["tool_calls"]; ok {
				hasTool = true
			}
			content := asString(delta["content"])
			if content != "" {
				hasText = true
			}
			if strings.Contains(strings.ToLower(content), `"tool_calls"`) {
				rawLeak = true
			}
		}
	}
	cc.assert("tool_calls_delta_present", hasTool, "tool_calls delta missing")
	cc.assert("no_raw_tool_json_leak", !rawLeak, "raw tool_calls leaked")
	cc.assert("done_terminated", done, "expected [DONE]")
	if !hasTool || !hasText {
		r.warnings = append(r.warnings, "toolcall mixed stream did not produce both text and tool_calls in this run (model-side behavior dependent)")
	}
	return nil
}

func (r *Runner) caseSSEJSONIntegrity(ctx context.Context, cc *caseContext) error {
	openaiResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-v4-flash",
			"messages": []map[string]any{
				{"role": "user", "content": "输出一句话"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("openai_status_200", openaiResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", openaiResp.StatusCode))
	badOpenAI := countMalformedSSEJSONLines(openaiResp.Body)
	cc.assert("openai_sse_json_valid", badOpenAI == 0, fmt.Sprintf("malformed=%d", badOpenAI))

	anthropicResp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/anthropic/v1/messages",
		Headers: map[string]string{
			"Authorization":     "Bearer " + r.apiKey,
			"anthropic-version": "2023-06-01",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-5",
			"messages": []map[string]any{
				{"role": "user", "content": "stream json integrity"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: false,
	})
	if err != nil {
		return err
	}
	cc.assert("anthropic_status_200", anthropicResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", anthropicResp.StatusCode))
	badAnthropic := countMalformedSSEJSONLines(anthropicResp.Body)
	cc.assert("anthropic_sse_json_valid", badAnthropic == 0, fmt.Sprintf("malformed=%d", badAnthropic))
	return nil
}

func countMalformedSSEJSONLines(body []byte) int {
	lines := strings.Split(string(body), "\n")
	bad := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(payload), &v); err != nil {
			bad++
		}
	}
	return bad
}
