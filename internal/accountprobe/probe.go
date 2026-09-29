package accountprobe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"whale2api/internal/auth"
	"whale2api/internal/completionruntime"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/poolaccounthealth"
	"whale2api/internal/pooldb"
	"whale2api/internal/promptcompat"
	"whale2api/internal/sse"
)

const (
	defaultProbeModel  = "deepseek-flash"
	DefaultProbePrompt = "Reply with only: pong"
)

// Result is the outcome of login + minimal completion probe against DeepSeek.
type Result struct {
	OK            bool
	Message       string
	Token         string
	PoolStatus    string // active | muted | banned
	DiscardReason string // pooldb discard reason when account should be marked discarded
	AutoDiscard   bool
	MuteUntil     *time.Time
}

// LoginFunc authenticates an account and returns an upstream token.
// auth.Resolver.LoginWithDevice satisfies it and adds on-demand device token
// harvesting plus a one-shot retry after device rejection.
type LoginFunc func(ctx context.Context, acc config.Account) (string, error)

// Probe logs in and sends a short completion to classify account health.
func Probe(ctx context.Context, ds *dsclient.Client, acc config.Account, prompt string) Result {
	return ProbeWithLogin(ctx, ds, nil, acc, prompt)
}

// ProbeWithLogin is Probe with a custom login function (nil falls back to ds.Login).
func ProbeWithLogin(ctx context.Context, ds *dsclient.Client, login LoginFunc, acc config.Account, prompt string) Result {
	if strings.TrimSpace(prompt) == "" {
		prompt = DefaultProbePrompt
	}
	if login == nil {
		login = ds.Login
	}
	ident := strings.TrimSpace(acc.Identifier())
	token, err := login(ctx, acc)
	if err != nil {
		// Device-token failures are neither mute/ban nor transient: never mark
		// the account available or discard it because of them.
		if errors.Is(err, auth.ErrDeviceHarvestUnavailable) {
			return Result{OK: false, Message: "device harvest service unavailable: " + err.Error()}
		}
		if errors.Is(err, auth.ErrNoUsableDeviceToken) || dsclient.IsDeviceRejectedError(err) {
			return Result{OK: false, Message: err.Error()}
		}
		if reason := poolaccounthealth.ClassifyLoginError(err.Error()); reason != "" {
			return Result{
				OK:            false,
				Message:       err.Error(),
				PoolStatus:    reason,
				DiscardReason: reason,
				AutoDiscard:   true,
			}
		}
		if poolaccounthealth.IsTransientProbeError(err.Error()) {
			return probeOK(strings.TrimSpace(acc.Token))
		}
		return Result{OK: false, Message: err.Error()}
	}

	a := &auth.RequestAuth{
		AccountID:      ident,
		DeepSeekToken:  token,
		UseConfigToken: false,
		TriedAccounts:  map[string]bool{},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(map[string]any{
		"model": defaultProbeModel,
		"messages": []any{
			map[string]any{"role": "user", "content": prompt},
		},
		"stream": false,
	}, "account-probe")
	if err != nil {
		return Result{OK: false, Message: err.Error(), Token: token}
	}

	start, outErr := completionruntime.StartCompletion(ctx, ds, a, stdReq, completionruntime.Options{MaxAttempts: 3})
	if outErr != nil {
		if poolaccounthealth.IsTransientProbeError(outErr.Message) {
			return probeOK(token)
		}
		return Result{OK: false, Message: outErr.Message, Token: token}
	}
	if start.Response == nil || start.Response.Body == nil {
		return Result{OK: false, Message: "empty completion response", Token: token}
	}
	defer func() { _ = start.Response.Body.Close() }()

	raw, err := io.ReadAll(start.Response.Body)
	if kind, msg := poolaccounthealth.ClassifyResponseBytes(raw); kind != "" {
		var muteUntil *time.Time
		if kind == pooldb.DiscardReasonMuted {
			muteUntil = poolaccounthealth.MuteUntilFromResponseBytes(raw)
		}
		return Result{
			OK:            false,
			Message:       msg,
			Token:         token,
			PoolStatus:    kind,
			DiscardReason: kind,
			AutoDiscard:   true,
			MuteUntil:     muteUntil,
		}
	}
	if err != nil {
		if poolaccounthealth.IsTransientProbeError(err.Error()) {
			return probeOK(token)
		}
		return Result{OK: false, Message: err.Error(), Token: token}
	}

	resp := &http.Response{
		StatusCode: start.Response.StatusCode,
		Body:       io.NopCloser(strings.NewReader(string(raw))),
		Header:     make(http.Header),
	}
	collected := sse.CollectStream(resp, stdReq.Thinking, true)
	if strings.TrimSpace(collected.Text) == "" {
		if collected.ContentFilter {
			return Result{
				OK:            false,
				Message:       "content filtered",
				Token:         token,
				PoolStatus:    pooldb.DiscardReasonBanned,
				DiscardReason: pooldb.DiscardReasonBanned,
				AutoDiscard:   true,
			}
		}
		// Empty body without mute/ban signals: treat as available (often flaky upstream SSE).
		return probeOK(token)
	}
	return probeOK(token)
}

func probeOK(token string) Result {
	return Result{
		OK:         true,
		Message:    "available",
		Token:      token,
		PoolStatus: "active",
	}
}
