package testsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func (r *Runner) caseInvalidModel(ctx context.Context, cc *caseContext) error {
	resp, err := cc.requestOnce(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model": "deepseek-not-exists",
			"messages": []map[string]any{
				{"role": "user", "content": "hi"},
			},
			"stream": false,
		},
		Retryable: false,
	}, 1)
	if err != nil {
		return err
	}
	cc.assert("status_503", resp.StatusCode == http.StatusServiceUnavailable, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_type_service_unavailable", asString(e["type"]) == "service_unavailable_error", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}

func (r *Runner) caseMissingMessages(ctx context.Context, cc *caseContext) error {
	resp, err := cc.request(ctx, requestSpec{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]string{
			"Authorization": "Bearer " + r.apiKey,
		},
		Body: map[string]any{
			"model":  "deepseek-v4-flash",
			"stream": false,
		},
		Retryable: true,
	})
	if err != nil {
		return err
	}
	cc.assert("status_400", resp.StatusCode == http.StatusBadRequest, fmt.Sprintf("status=%d", resp.StatusCode))
	var m map[string]any
	_ = json.Unmarshal(resp.Body, &m)
	e, _ := m["error"].(map[string]any)
	cc.assert("error_type_invalid_request", asString(e["type"]) == "invalid_request_error", fmt.Sprintf("body=%s", string(resp.Body)))
	return nil
}
