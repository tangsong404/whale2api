package testsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"whale2api/internal/config"
)

func (r *Runner) caseHealthz(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/healthz", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("status_ok", asString(m["status"]) == "ok", fmt.Sprintf("body=%s", string(resp.Body)))

	headResp, headErr := cc.request(ctx, requestSpec{Method: http.MethodHead, Path: "/healthz", Retryable: true})
	if headErr != nil {
		return headErr
	}
	cc.assert("head_status_200", headResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", headResp.StatusCode))
	return nil
}

func (r *Runner) caseReadyz(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/readyz", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("status_ready", asString(m["status"]) == "ready", fmt.Sprintf("body=%s", string(resp.Body)))

	headResp, headErr := cc.request(ctx, requestSpec{Method: http.MethodHead, Path: "/readyz", Retryable: true})
	if headErr != nil {
		return headErr
	}
	cc.assert("head_status_200", headResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", headResp.StatusCode))
	return nil
}

func (r *Runner) caseModelsOpenAI(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/v1/models", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	ids := extractModelIDs(resp.Body)
	cc.assert("has_deepseek_flash", contains(ids, "deepseek-v4-flash"), strings.Join(ids, ","))
	cc.assert("no_deepseek_pro", !contains(ids, "deepseek-v4-pro"), strings.Join(ids, ","))
	cc.assert("only_one_model", len(ids) == 1, strings.Join(ids, ","))
	cc.assert("no_nothinking_skus", !contains(ids, "deepseek-v4-flash-nothinking") && !contains(ids, "deepseek-v4-pro-nothinking"), strings.Join(ids, ","))
	cc.assert("no_retired_skus", !contains(ids, "deepseek-v4-vision"), strings.Join(ids, ","))
	assertListModelsContextLength(cc, resp.Body, config.AdvertisedMaxContextTokens)
	return nil
}

func (r *Runner) caseModelOpenAIByID(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{Method: http.MethodGet, Path: "/v1/models/deepseek-v4-flash", Retryable: true})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_model", asString(m["object"]) == "model", fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("id_deepseek_chat", asString(m["id"]) == "deepseek-v4-flash", fmt.Sprintf("body=%s", string(resp.Body)))
	got, ok := jsonNumberToInt(m["context_length"])
	cc.assert("context_length", ok && got == config.AdvertisedMaxContextTokens, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}
func (r *Runner) caseChatNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-v4-flash",
			"messages": []map[string]any{
				{"role": "user", "content": "请简单回复一句话"},
			},
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_chat_completion", asString(m["object"]) == "chat.completion", fmt.Sprintf("body=%s", string(resp.Body)))
	choices, _ := m["choices"].([]any)
	cc.assert("choices_non_empty", len(choices) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseChatStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-v4-flash",
			"messages": []map[string]any{
				{"role": "user", "content": "请流式回复一句话"},
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
	cc.assert("frames_non_empty", len(frames) > 0, fmt.Sprintf("len=%d", len(frames)))
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseResponsesNonstream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "gpt-4o",
			"input": "请简要回答 hello",
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200", resp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	cc.assert("object_response", asString(m["object"]) == "response", fmt.Sprintf("body=%s", string(resp.Body)))
	responseID := asString(m["id"])
	cc.assert("response_id_present", responseID != "", fmt.Sprintf("body=%s", string(resp.Body)))
	if responseID != "" {
		getResp, getErr := cc.request(ctx, requestSpec{
			Method: http.MethodGet,
			Path:   "/v1/responses/" + responseID,
			Headers: map[string]string{
				"Authorization": "Bearer " + r.apiKey,
			},
			Retryable: true,
		})
		if getErr != nil {
			return getErr
		}
		cc.assert("get_status_200", getResp.StatusCode == http.StatusOK, fmt.Sprintf("status=%d", getResp.StatusCode))
	}
	return nil
}

func (r *Runner) caseResponsesStream(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model":  "gpt-4o",
			"input":  "请流式回答 hello",
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
	cc.assert("frames_non_empty", len(frames) > 0, fmt.Sprintf("len=%d", len(frames)))
	hasCreated := false
	hasCompleted := false
	for _, f := range frames {
		switch asString(f["type"]) {
		case "response.created":
			hasCreated = true
		case "response.completed":
			hasCompleted = true
		}
	}
	cc.assert("has_response_created", hasCreated, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("has_response_completed", hasCompleted, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("done_terminated", done, "expected [DONE]")
	return nil
}

func (r *Runner) caseEmbeddings(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/embeddings",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "gpt-4o",
			"input": []string{"hello", "world"},
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_200_or_501", resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotImplemented, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	if resp.StatusCode == http.StatusOK {
		cc.assert("object_list", asString(m["object"]) == "list", fmt.Sprintf("body=%s", string(resp.Body)))
		data, _ := m["data"].([]any)
		cc.assert("data_non_empty", len(data) > 0, fmt.Sprintf("body=%s", string(resp.Body)))
		return nil
	}
	errObj, _ := m["error"].(map[string]any)
	_, hasCode := errObj["code"]
	_, hasParam := errObj["param"]
	cc.assert("error_has_code", hasCode, fmt.Sprintf("body=%s", string(resp.Body)))
	cc.assert("error_has_param", hasParam, fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}
