package poolui

import (
	"context"
	"sync"
	"sync/atomic"
)

type testJobRunner struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	cancelled atomic.Bool
}

var testRunners sync.Map // api_key -> *testJobRunner

func testRunnerFor(apiKey string) *testJobRunner {
	v, _ := testRunners.LoadOrStore(apiKey, &testJobRunner{})
	return v.(*testJobRunner)
}

func (r *testJobRunner) start() context.Context {
	if r == nil {
		return context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	r.cancelled.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	return ctx
}

func (r *testJobRunner) stop() {
	if r == nil {
		return
	}
	r.cancelled.Store(true)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *testJobRunner) isCancelled() bool {
	if r == nil {
		return false
	}
	return r.cancelled.Load()
}

func (r *testJobRunner) clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancel = nil
	r.cancelled.Store(false)
}

func (s *Server) isTestJobCancelled(apiKey string) bool {
	return testRunnerFor(apiKey).isCancelled()
}
