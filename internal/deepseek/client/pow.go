package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"whale2api/pow"
)

// ComputePow 使用纯 Go 实现求解 PoW challenge (DeepSeekHashV1)。
func ComputePow(ctx context.Context, challenge map[string]any) (int64, error) {
	algo, _ := challenge["algorithm"].(string)
	if algo != "DeepSeekHashV1" {
		return 0, errors.New("unsupported algorithm")
	}
	challengeStr, _ := challenge["challenge"].(string)
	salt, _ := challenge["salt"].(string)
	expireAt := toInt64(challenge["expire_at"], 1680000000)
	difficulty := toInt64FromFloat(challenge["difficulty"], 144000)

	return pow.SolvePow(ctx, challengeStr, salt, expireAt, difficulty)
}

// BuildPowHeader 序列化 {algorithm,challenge,salt,answer,signature,target_path} 为 base64(JSON)。
func BuildPowHeader(challenge map[string]any, answer int64) (string, error) {
	payload := map[string]any{
		"algorithm":   challenge["algorithm"],
		"challenge":   challenge["challenge"],
		"salt":        challenge["salt"],
		"answer":      answer,
		"signature":   challenge["signature"],
		"target_path": challenge["target_path"],
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func toFloat64(v any, d float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return d
	}
}

func toInt64(v any, d int64) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return d
	}
}

// toInt64FromFloat 与 toInt64 等价，仅名称区分用途。
func toInt64FromFloat(v any, d int64) int64 {
	return toInt64(v, d)
}
