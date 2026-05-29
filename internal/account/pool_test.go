package account

import (
	"context"
	"sync"
	"testing"
	"time"

	"whale2api/internal/config"
)

func newPoolForTest(t *testing.T, maxInflight string) *Pool {
	t.Helper()
	t.Setenv("WHALE2API_ACCOUNT_MAX_INFLIGHT", maxInflight)
	t.Setenv("WHALE2API_ACCOUNT_MAX_QUEUE", "")
	return newTestPool([]config.Account{
		{Email: "acc1@example.com", Token: "token1"},
		{Email: "acc2@example.com", Token: "token2"},
	}, config.LoadStore())
}

func newSingleAccountPoolForTest(t *testing.T, maxInflight string) *Pool {
	t.Helper()
	t.Setenv("WHALE2API_ACCOUNT_MAX_INFLIGHT", maxInflight)
	t.Setenv("WHALE2API_ACCOUNT_MAX_QUEUE", "")
	return newTestPool([]config.Account{
		{Email: "acc1@example.com", Token: "token1"},
	}, config.LoadStore())
}

func waitForWaitingCount(t *testing.T, pool *Pool, want int) {
	t.Helper()
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		status := pool.Status()
		if got, ok := status["waiting"].(int); ok && got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := pool.Status()
	t.Fatalf("waiting count did not reach %d, current status=%v", want, status)
}

func TestPoolRoundRobinWithConcurrentSlots(t *testing.T) {
	pool := newPoolForTest(t, "2")

	order := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		acc, ok := pool.Acquire("", nil)
		if !ok {
			t.Fatalf("expected acquire success at step %d", i+1)
		}
		order = append(order, acc.Identifier())
	}
	want := []string{"acc1@example.com", "acc2@example.com", "acc1@example.com", "acc2@example.com"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("unexpected order at %d: got %q want %q (full=%v)", i, order[i], want[i], order)
		}
	}

	if _, ok := pool.Acquire("", nil); ok {
		t.Fatalf("expected acquire to fail when all inflight slots are occupied")
	}

	pool.Release("acc1@example.com")
	acc, ok := pool.Acquire("", nil)
	if !ok || acc.Identifier() != "acc1@example.com" {
		t.Fatalf("expected reacquire acc1 after releasing one slot, got ok=%v id=%q", ok, acc.Identifier())
	}
}

func TestPoolTargetAccountInflightLimit(t *testing.T) {
	pool := newPoolForTest(t, "2")

	for i := 0; i < 2; i++ {
		if _, ok := pool.Acquire("acc1@example.com", nil); !ok {
			t.Fatalf("expected target acquire success at step %d", i+1)
		}
	}
	if _, ok := pool.Acquire("acc1@example.com", nil); ok {
		t.Fatalf("expected third acquire on same target to fail due to inflight limit")
	}
}

func TestPoolConcurrentAcquireDistribution(t *testing.T) {
	pool := newPoolForTest(t, "2")

	start := make(chan struct{})
	results := make(chan string, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			acc, ok := pool.Acquire("", nil)
			if !ok {
				results <- "FAIL"
				return
			}
			results <- acc.Identifier()
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	success := 0
	fail := 0
	perAccount := map[string]int{}
	for id := range results {
		if id == "FAIL" {
			fail++
			continue
		}
		success++
		perAccount[id]++
	}
	if success != 4 || fail != 2 {
		t.Fatalf("unexpected concurrent acquire result: success=%d fail=%d perAccount=%v", success, fail, perAccount)
	}
	for id, n := range perAccount {
		if n > 2 {
			t.Fatalf("account %s exceeded inflight limit: %d", id, n)
		}
	}
}

func TestPoolStatusRecommendedConcurrencyDefault(t *testing.T) {
	pool := newPoolForTest(t, "")
	status := pool.Status()

	if got, ok := status["max_inflight_per_account"].(int); !ok || got != 1 {
		t.Fatalf("unexpected max_inflight_per_account: %#v", status["max_inflight_per_account"])
	}
	if got, ok := status["recommended_concurrency"].(int); !ok || got != 2 {
		t.Fatalf("unexpected recommended_concurrency: %#v", status["recommended_concurrency"])
	}
	if got, ok := status["max_queue_size"].(int); !ok || got != 2 {
		t.Fatalf("unexpected max_queue_size: %#v", status["max_queue_size"])
	}
}

func TestPoolStatusRecommendedConcurrencyRespectsOverride(t *testing.T) {
	pool := newPoolForTest(t, "3")
	status := pool.Status()

	if got, ok := status["max_inflight_per_account"].(int); !ok || got != 3 {
		t.Fatalf("unexpected max_inflight_per_account: %#v", status["max_inflight_per_account"])
	}
	if got, ok := status["recommended_concurrency"].(int); !ok || got != 6 {
		t.Fatalf("unexpected recommended_concurrency: %#v", status["recommended_concurrency"])
	}
	if got, ok := status["max_queue_size"].(int); !ok || got != 6 {
		t.Fatalf("unexpected max_queue_size: %#v", status["max_queue_size"])
	}
}

func TestPoolGlobalMaxInflightEnv(t *testing.T) {
	t.Setenv("WHALE2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("WHALE2API_GLOBAL_MAX_INFLIGHT", "4")

	pool := newTestPool([]config.Account{
		{Email: "acc1@example.com", Token: "token1"},
		{Email: "acc2@example.com", Token: "token2"},
	}, config.LoadStore())
	status := pool.Status()
	if got, ok := status["global_max_inflight"].(int); !ok || got != 4 {
		t.Fatalf("unexpected global_max_inflight: %#v", status["global_max_inflight"])
	}
	if got, ok := status["max_inflight_per_account"].(int); !ok || got != 1 {
		t.Fatalf("unexpected max_inflight_per_account: %#v", status["max_inflight_per_account"])
	}
	if got, ok := status["recommended_concurrency"].(int); !ok || got != 2 {
		t.Fatalf("unexpected recommended_concurrency: %#v", status["recommended_concurrency"])
	}
}

func TestPoolDropsLegacyTokenOnlyAccountOnLoad(t *testing.T) {
	t.Setenv("WHALE2API_ACCOUNT_MAX_INFLIGHT", "1")

	pool := newTestPool([]config.Account{{Token: "token-only-account"}}, config.LoadStore())
	status := pool.Status()
	if got, ok := status["total"].(int); !ok || got != 0 {
		t.Fatalf("unexpected total in pool status: %#v", status["total"])
	}
	if got, ok := status["available"].(int); !ok || got != 0 {
		t.Fatalf("unexpected available in pool status: %#v", status["available"])
	}

	if _, ok := pool.Acquire("", nil); ok {
		t.Fatalf("expected acquire to fail for token-only account")
	}
}

func TestPoolAcquireRotatesIntoTokenlessAccounts(t *testing.T) {
	t.Setenv("WHALE2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("WHALE2API_ACCOUNT_MAX_QUEUE", "")

	pool := newTestPool([]config.Account{
		{Email: "acc1@example.com", Token: "token1"},
		{Email: "acc2@example.com"},
		{Email: "acc3@example.com"},
	}, config.LoadStore())
	for i, want := range []string{"acc1@example.com", "acc2@example.com", "acc3@example.com"} {
		acc, ok := pool.Acquire("", nil)
		if !ok {
			t.Fatalf("expected acquire success at step %d", i+1)
		}
		if got := acc.Identifier(); got != want {
			t.Fatalf("unexpected account at step %d: got %q want %q", i+1, got, want)
		}
		pool.Release(acc.Identifier())
	}
}

func TestPoolAcquireWaitQueuesAndSucceedsAfterRelease(t *testing.T) {
	pool := newSingleAccountPoolForTest(t, "1")
	first, ok := pool.Acquire("", nil)
	if !ok {
		t.Fatal("expected first acquire to succeed")
	}

	type result struct {
		id string
		ok bool
	}
	resCh := make(chan result, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		acc, ok := pool.AcquireWait(ctx, "", nil)
		resCh <- result{id: acc.Identifier(), ok: ok}
	}()

	waitForWaitingCount(t, pool, 1)
	pool.Release(first.Identifier())

	select {
	case res := <-resCh:
		if !res.ok {
			t.Fatal("expected queued acquire to succeed after release")
		}
		if res.id != "acc1@example.com" {
			t.Fatalf("unexpected account id from queued acquire: %q", res.id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued acquire result")
	}
}

func TestPoolAcquireWaitQueueLimitReturnsFalse(t *testing.T) {
	pool := newSingleAccountPoolForTest(t, "1")
	first, ok := pool.Acquire("", nil)
	if !ok {
		t.Fatal("expected first acquire to succeed")
	}

	type result struct {
		id string
		ok bool
	}
	firstWaiter := make(chan result, 1)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel1()
	go func() {
		acc, ok := pool.AcquireWait(ctx1, "", nil)
		firstWaiter <- result{id: acc.Identifier(), ok: ok}
	}()
	waitForWaitingCount(t, pool, 1)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	start := time.Now()
	if _, ok := pool.AcquireWait(ctx2, "", nil); ok {
		t.Fatal("expected second queued acquire to fail when queue is full")
	}
	if time.Since(start) > 120*time.Millisecond {
		t.Fatalf("queue-full acquire should fail fast, took %s", time.Since(start))
	}

	pool.Release(first.Identifier())
	select {
	case res := <-firstWaiter:
		if !res.ok {
			t.Fatal("expected first queued acquire to succeed after release")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first queued acquire")
	}
}
