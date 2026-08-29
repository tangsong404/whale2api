package shared

import "encoding/json"

// CloneJSONMap copies a decoded JSON request so account-scoped preprocessing
// can be restarted after replacing a discarded account.
func CloneJSONMap(src map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]any
	if err := json.Unmarshal(raw, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}
