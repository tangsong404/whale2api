package testsuite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
)

func parseSSEFrames(body []byte) ([]map[string]any, bool) {
	lines := strings.Split(string(body), "\n")
	frames := make([]map[string]any, 0, len(lines))
	done := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err == nil {
			frames = append(frames, m)
		}
	}
	return frames, done
}

func parseClaudeStreamEvents(body []byte) []string {
	events := []string{}
	seen := map[string]bool{}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		t := asString(m["type"])
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		events = append(events, t)
	}
	return events
}

func extractModelIDs(body []byte) []string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil
	}
	out := []string{}
	data, _ := m["data"].([]any)
	for _, it := range data {
		item, _ := it.(map[string]any)
		id := asString(item["id"])
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func jsonNumberToInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	default:
		return 0, false
	}
}

func assertListModelsContextLength(cc *caseContext, body []byte, want int) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		cc.assert("models_json_unmarshal", false, err.Error())
		return
	}
	data, ok := m["data"].([]any)
	if !ok {
		cc.assert("models_data_array", false, fmt.Sprintf("got %T", m["data"]))
		return
	}
	for _, it := range data {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id := asString(item["id"])
		got, ok := jsonNumberToInt(item["context_length"])
		cc.assert("context_length_"+id, ok && got == want, fmt.Sprintf("id=%s got=%d want=%d raw=%v", id, got, want, item["context_length"]))
	}
}

func withTraceQuery(rawURL, traceID string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("__trace_id", traceID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func prepareServerEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	skip := map[string]struct{}{}
	for k := range overrides {
		skip[k] = struct{}{}
	}
	for _, e := range base {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if _, ok := skip[parts[0]]; ok {
			continue
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to detect tcp port")
	}
	return addr.Port, nil
}

func uniqueStatusCodes(in []responseLog) []int {
	set := map[int]struct{}{}
	for _, it := range in {
		if it.StatusCode > 0 {
			set[it.StatusCode] = struct{}{}
		}
	}
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func has5xx(dist map[int]int) (int, bool) {
	for k := range dist {
		if k >= 500 {
			return k, true
		}
	}
	return 0, false
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case float32:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
