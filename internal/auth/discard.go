package auth

import (
	"context"
	"strings"

	"whale2api/internal/config"
	"whale2api/internal/poolaccounthealth"
)

// TryAutoDiscardFromError marks the current pooled account discarded when err indicates mute/ban.
func (r *Resolver) TryAutoDiscardFromError(ctx context.Context, a *RequestAuth, err error) bool {
	if err == nil {
		return false
	}
	return r.TryAutoDiscardFromMessage(ctx, a, err.Error())
}

// TryAutoDiscardFromMessage classifies a plain error string for mute/ban.
func (r *Resolver) TryAutoDiscardFromMessage(ctx context.Context, a *RequestAuth, msg string) bool {
	reason := poolaccounthealth.ClassifyLoginError(msg)
	if reason == "" {
		return false
	}
	return r.applyAutoDiscard(ctx, a, reason)
}

// TryAutoDiscardFromHTTPBody classifies a raw HTTP response body for mute/ban.
func (r *Resolver) TryAutoDiscardFromHTTPBody(ctx context.Context, a *RequestAuth, raw []byte) bool {
	reason, _ := poolaccounthealth.ClassifyResponseBytes(raw)
	if reason == "" {
		return false
	}
	return r.applyAutoDiscard(ctx, a, reason)
}

// TryAutoDiscardFromDeepSeekMap classifies a DeepSeek JSON envelope for mute/ban.
func (r *Resolver) TryAutoDiscardFromDeepSeekMap(ctx context.Context, a *RequestAuth, resp map[string]any) bool {
	reason, _ := poolaccounthealth.ClassifyResponseMap(resp)
	if reason == "" {
		return false
	}
	return r.applyAutoDiscard(ctx, a, reason)
}

func (r *Resolver) applyAutoDiscard(ctx context.Context, a *RequestAuth, reason string) bool {
	if r == nil || a == nil || !a.UseConfigToken || !a.PoolManaged {
		return false
	}
	apiKey := strings.TrimSpace(a.GatewayAPIKey)
	ident := strings.TrimSpace(a.AccountID)
	if apiKey == "" || ident == "" {
		return false
	}
	admin, ok := r.PoolDB.(PoolAdmin)
	if !ok || admin == nil {
		return false
	}
	if err := admin.SetAccountPoolState(ctx, apiKey, ident, true, reason); err != nil {
		config.Logger.Warn("[pool] auto-discard failed", "api_key", apiKey, "account", ident, "reason", reason, "error", err)
		return false
	}
	config.Logger.Info("[pool] auto-discarded account", "api_key", apiKey, "account", ident, "reason", reason)
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	a.TriedAccounts[ident] = true
	return true
}
