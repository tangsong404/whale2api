package testsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

func (r *Runner) caseReasonerStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-v4-pro",
			"messages": []map[string]any{
				{"role": "user", "content": "先思考后回答：1+1"},
			},
			"stream": true,
		},
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasReasoning := false
	for _, f := range frames {
		choices, _ := f["choices"].([]any)
		for _, c := range choices {
			ch, _ := c.(map[string]any)
			delta, _ := ch["delta"].(map[string]any)
			if asString(delta["reasoning_content"]) != "" {
				hasReasoning = true
			}
		}
	}
	cc.assert("has_reasoning_content", hasReasoning, "reasoning_content not found")
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseToolcallNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      toolcallPayload(false),
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		cc.assert("choices_non_empty", false, fmt.Sprintf("body=%s", string(resp.Body)))
		return nil
	}
	c0, _ := choices[0].(map[string]any)
	cc.assert("finish_reason_tool_calls", asString(c0["finish_reason"]) == "tool_calls", fmt.Sprintf("body=%s", string(resp.Body)))
	msg, _ := c0["message"].(map[string]any)
	tc, _ := msg["tool_calls"].([]any)
	cc.assert("tool_calls_present", len(tc) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseToolcallStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body:      toolcallPayload(true),
		Stream:    true,
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	frames, done := parseSSEFrames(resp.Body)
	hasTool := false
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
			if strings.Contains(strings.ToLower(content), `"tool_calls"`) {
				rawLeak = true
			}
		}
	}
	cc.assert("tool_calls_delta_present", hasTool, "tool_calls delta missing")
	cc.assert("no_raw_tool_json_leak", !rawLeak, "raw tool_calls JSON leaked in content")
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseConcurrencyBurst(ctx context.Context, cc *caseContext) error {
	accountCount := len(r.configRaw.Accounts)
	n := accountCount*2 + 2
	if n < 2 {
		n = 2
	}
	type one struct {
		Status int
		Err    string
	}
	results := make([]one, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := cc.request(ctx, requestSpec{
				Method: http.MethodPost,
				Path:   "/v1/chat/completions",
				Headers: map[string]string{
					"Authorization": "Bearer " + r.apiKey,
				},
				Body: map[string]any{
					"model": "deepseek-v4-flash",
					"messages": []map[string]any{
						{"role": "user", "content": fmt.Sprintf("并发请求 #%d，请回复ok", idx)},
					},
					"stream": true,
				},
				Stream:    true,
				Retryable: true,
			})
			if err != nil {
				results[idx] = one{Err: err.Error()}
				return
			}
			results[idx] = one{Status: resp.StatusCode}
		}(i)
	}
	wg.Wait()

	dist := map[int]int{}
	success := 0
	for _, it := range results {
		if it.Status > 0 {
			dist[it.Status]++
			if it.Status == http.StatusOK {
				success++
			}
		}
	}
	cc.assert("success_gt_zero", success > 0, fmt.Sprintf("distribution=%v", dist))
	_, has5xx := has5xx(dist)
	cc.assert("no_5xx", !has5xx, fmt.Sprintf("distribution=%v", dist))
	if err := r.ping("/healthz"); err != nil {
		cc.assert("server_alive", false, err.Error())
	} else {
		cc.assert("server_alive", true, "")
	}
	return nil
}

func (r *Runner) caseInvalidKey(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer invalid-testsuite-key-" + sanitizeID(r.runID),
		},
		Body: map[string]any{
			"model": "deepseek-v4-flash",
			"messages": []map[string]any{
				{"role": "user", "content": "hi"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_401", resp.StatusCode == http.StatusUnauthorized, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_object_present", len(e) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("error_message_present", asString(e["message"]) != "", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func toolcallPayload(stream bool) map[string]any {
	return map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": "你必须调用工具 search 查询 golang，并仅返回工具调用。",
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search documents",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"q": map[string]any{
								"type": "string",
							},
						},
						"required": []string{"q"},
					},
				},
			},
		},
		"stream": stream,
	}
}
