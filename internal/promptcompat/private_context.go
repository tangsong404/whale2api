package promptcompat

import "strings"

const PrivateContextLivePrompt = "提供済みの先行会話を踏まえ、ユーザーの最新の依頼に直接応えること。"

func SplitMessagesForPrivateContext(messages []any) (livePrefix, history []any) {
	if len(messages) == 0 {
		return nil, nil
	}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := normalizeOpenAIRoleForPrompt(strings.ToLower(strings.TrimSpace(asString(msg["role"]))))
		if role == "system" {
			livePrefix = append(livePrefix, msg)
			continue
		}
		history = append(history, msg)
	}
	return livePrefix, history
}

func BuildPrivateContextLiveMessages(messages []any) []any {
	livePrefix, _ := SplitMessagesForPrivateContext(messages)
	out := make([]any, 0, len(livePrefix)+1)
	out = append(out, livePrefix...)
	out = append(out, map[string]any{
		"role":    "user",
		"content": PrivateContextLivePrompt,
	})
	return out
}
