package stream

import (
	"context"
	"io"
	"time"

	"whale2api/internal/sse"
)

type StopReason string

const (
	StopReasonNone              StopReason = ""
	StopReasonContextCancelled  StopReason = "context_cancelled"
	StopReasonNoContentTimeout  StopReason = "no_content_timeout"
	StopReasonIdleTimeout       StopReason = "idle_timeout"
	StopReasonUpstreamCompleted StopReason = "upstream_completed"
	StopReasonHandlerRequested  StopReason = "handler_requested"
)

type ConsumeConfig struct {
	Context             context.Context
	Body                io.Reader
	ThinkingEnabled     bool
	InitialType         string
	KeepAliveInterval   time.Duration
	IdleTimeout         time.Duration
	MaxKeepAliveNoInput int
}

type ParsedDecision struct {
	Stop        bool
	StopReason  StopReason
	ContentSeen bool
}

type ConsumeHooks struct {
	OnParsed      func(parsed sse.LineResult) ParsedDecision
	OnKeepAlive   func()
	OnFinalize    func(reason StopReason, scannerErr error)
	OnContextDone func()
}

func ConsumeSSE(cfg ConsumeConfig, hooks ConsumeHooks) {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	initialType := cfg.InitialType
	if initialType == "" {
		if cfg.ThinkingEnabled {
			initialType = "thinking"
		} else {
			initialType = "text"
		}
	}
	parsedLines, done := sse.StartParsedLinePump(cfg.Context, cfg.Body, cfg.ThinkingEnabled, initialType)

	var ticker *time.Ticker
	if cfg.KeepAliveInterval > 0 {
		ticker = time.NewTicker(cfg.KeepAliveInterval)
		defer ticker.Stop()
	}

	hasContent := false
	lastContent := time.Now()
	keepaliveCount := 0

	finalize := func(reason StopReason, scannerErr error) {
		if hooks.OnFinalize != nil {
			hooks.OnFinalize(reason, scannerErr)
		}
	}
	contextDone := func() bool {
		if cfg.Context.Err() == nil {
			return false
		}
		if hooks.OnContextDone != nil {
			hooks.OnContextDone()
		}
		return true
	}

	for {
		if contextDone() {
			return
		}
		select {
		case <-cfg.Context.Done():
			if contextDone() {
				return
			}
			return
		case <-tickCh(ticker):
			if contextDone() {
				return
			}
			if !hasContent {
				keepaliveCount++
				if cfg.MaxKeepAliveNoInput > 0 && keepaliveCount >= cfg.MaxKeepAliveNoInput {
					finalize(StopReasonNoContentTimeout, nil)
					return
				}
			}
			if hasContent && cfg.IdleTimeout > 0 && time.Since(lastContent) > cfg.IdleTimeout {
				finalize(StopReasonIdleTimeout, nil)
				return
			}
			if hooks.OnKeepAlive != nil {
				hooks.OnKeepAlive()
			}
		case parsed, ok := <-parsedLines:
			if contextDone() {
				return
			}
			if !ok {
				finalize(StopReasonUpstreamCompleted, <-done)
				return
			}
			if hooks.OnParsed == nil {
				continue
			}
			decision := hooks.OnParsed(parsed)
			if decision.ContentSeen {
				hasContent = true
				lastContent = time.Now()
				keepaliveCount = 0
			}
			if decision.Stop {
				reason := decision.StopReason
				if reason == StopReasonNone {
					reason = StopReasonHandlerRequested
				}
				finalize(reason, nil)
				return
			}
		}
	}
}

func tickCh(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
