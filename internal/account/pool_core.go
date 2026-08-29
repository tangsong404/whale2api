package account

import (
	"sort"
	"sync"
	"time"

	"whale2api/internal/config"
)

type Pool struct {
	lookup                 Lookup
	runtime                *config.Store // optional; limits from env when nil
	mu                     sync.Mutex
	queue                  []string
	inUse                  map[string]int
	waiters                []chan struct{}
	maxInflightPerAccount  int
	recommendedConcurrency int
	maxQueueSize           int
	globalMaxInflight      int
	blockedUntil           map[string]time.Time
}

const staleSnapshotGuard = 5 * time.Second

// NewPoolWithRuntime uses lookup for account discovery and runtime (may be nil) for limit knobs.
// AccountCount returns how many accounts participate in pooling (for auth loop bounds).
func (p *Pool) AccountCount() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue)
}

func NewPoolWithRuntime(lookup Lookup, runtime *config.Store) *Pool {
	maxPer := 2
	if runtime != nil {
		maxPer = runtime.RuntimeAccountMaxInflight()
	}
	p := &Pool{
		lookup:                lookup,
		runtime:               runtime,
		inUse:                 map[string]int{},
		blockedUntil:          map[string]time.Time{},
		maxInflightPerAccount: maxPer,
	}
	p.Reset()
	return p
}

func (p *Pool) Reset() {
	if p.lookup == nil {
		return
	}
	accounts := p.lookup.Accounts()
	sort.SliceStable(accounts, func(i, j int) bool {
		iHas := accounts[i].Token != ""
		jHas := accounts[j].Token != ""
		if iHas == jHas {
			return i < j
		}
		return iHas
	})
	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		id := a.Identifier()
		if id != "" {
			ids = append(ids, id)
		}
	}
	if p.runtime != nil {
		p.maxInflightPerAccount = p.runtime.RuntimeAccountMaxInflight()
	} else {
		p.maxInflightPerAccount = maxInflightFromEnv()
	}
	recommended := defaultRecommendedConcurrency(len(ids), p.maxInflightPerAccount)
	queueLimit := maxQueueFromEnv(recommended)
	globalLimit := recommended
	if p.runtime != nil {
		queueLimit = p.runtime.RuntimeAccountMaxQueue(recommended)
		globalLimit = p.runtime.RuntimeGlobalMaxInflight(recommended)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drainWaitersLocked()
	p.queue = ids
	p.inUse = map[string]int{}
	p.blockedUntil = map[string]time.Time{}
	p.recommendedConcurrency = recommended
	p.maxQueueSize = queueLimit
	p.globalMaxInflight = globalLimit
	config.Logger.Info(
		"[init_account_queue] initialized",
		"total", len(ids),
		"max_inflight_per_account", p.maxInflightPerAccount,
		"global_max_inflight", p.globalMaxInflight,
		"recommended_concurrency", p.recommendedConcurrency,
		"max_queue_size", p.maxQueueSize,
	)
}

// UpdateAccounts refreshes the catalog without resetting active leases,
// round-robin position, or queued callers.
func (p *Pool) UpdateAccounts(accounts []config.Account) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	filtered := make([]config.Account, 0, len(accounts))
	for _, acc := range accounts {
		id := acc.Identifier()
		until, blocked := p.blockedUntil[id]
		if blocked && now.Before(until) {
			continue
		}
		if blocked {
			delete(p.blockedUntil, id)
		}
		filtered = append(filtered, acc)
	}
	lookup := NewMemoryLookup(filtered)
	incoming := lookup.Accounts()
	active := make(map[string]struct{}, len(incoming))
	for _, acc := range incoming {
		if id := acc.Identifier(); id != "" {
			active[id] = struct{}{}
		}
	}

	oldQueue := append([]string(nil), p.queue...)
	oldMaxPer := p.maxInflightPerAccount
	oldMaxQueue := p.maxQueueSize
	oldGlobalMax := p.globalMaxInflight
	p.lookup = lookup

	queue := make([]string, 0, len(active))
	seen := make(map[string]struct{}, len(active))
	for _, id := range p.queue {
		if _, ok := active[id]; !ok {
			continue
		}
		queue = append(queue, id)
		seen[id] = struct{}{}
	}
	for _, acc := range incoming {
		id := acc.Identifier()
		if _, ok := seen[id]; id == "" || ok {
			continue
		}
		queue = append(queue, id)
		seen[id] = struct{}{}
	}
	p.queue = queue
	p.refreshLimitsLocked()
	if !sameAccountOrder(oldQueue, queue) || oldMaxPer != p.maxInflightPerAccount || oldMaxQueue != p.maxQueueSize || oldGlobalMax != p.globalMaxInflight {
		p.notifyAllWaitersLocked()
	}
}

func sameAccountOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Remove prevents new leases for accountID while preserving its active lease
// count until current callers release it.
func (p *Pool) Remove(accountID string) {
	if p == nil || accountID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedUntil == nil {
		p.blockedUntil = map[string]time.Time{}
	}
	p.blockedUntil[accountID] = time.Now().Add(staleSnapshotGuard)
	for i, id := range p.queue {
		if id != accountID {
			continue
		}
		p.queue = append(p.queue[:i], p.queue[i+1:]...)
		break
	}
	p.refreshLimitsLocked()
	p.notifyAllWaitersLocked()
}

func (p *Pool) refreshLimitsLocked() {
	if p.runtime != nil {
		p.maxInflightPerAccount = p.runtime.RuntimeAccountMaxInflight()
	}
	p.recommendedConcurrency = defaultRecommendedConcurrency(len(p.queue), p.maxInflightPerAccount)
	if p.runtime != nil {
		p.maxQueueSize = p.runtime.RuntimeAccountMaxQueue(p.recommendedConcurrency)
		p.globalMaxInflight = p.runtime.RuntimeGlobalMaxInflight(p.recommendedConcurrency)
	}
}

func (p *Pool) Release(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	count := p.inUse[accountID]
	if count <= 0 {
		return
	}
	if count == 1 {
		delete(p.inUse, accountID)
		p.notifyWaiterLocked()
		return
	}
	p.inUse[accountID] = count - 1
	p.notifyWaiterLocked()
}

func (p *Pool) Status() map[string]any {
	if p.lookup == nil {
		return map[string]any{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	available := make([]string, 0, len(p.queue))
	inUseAccounts := make([]string, 0, len(p.inUse))
	inUseSlots := 0
	for _, id := range p.queue {
		if p.inUse[id] < p.maxInflightPerAccount {
			available = append(available, id)
		}
	}
	for id, count := range p.inUse {
		if count > 0 {
			inUseAccounts = append(inUseAccounts, id)
			inUseSlots += count
		}
	}
	sort.Strings(inUseAccounts)
	return map[string]any{
		"available":                len(available),
		"in_use":                   inUseSlots,
		"total":                    len(p.queue),
		"available_accounts":       available,
		"in_use_accounts":          inUseAccounts,
		"max_inflight_per_account": p.maxInflightPerAccount,
		"global_max_inflight":      p.globalMaxInflight,
		"recommended_concurrency":  p.recommendedConcurrency,
		"waiting":                  len(p.waiters),
		"max_queue_size":           p.maxQueueSize,
	}
}
