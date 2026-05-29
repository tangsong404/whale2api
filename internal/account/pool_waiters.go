package account

func (p *Pool) canQueueLocked(target string, exclude map[string]bool) bool {
	if target != "" {
		if exclude[target] {
			return false
		}
		if _, ok := p.lookup.FindAccount(target); !ok {
			return false
		}
	}
	if p.maxQueueSize <= 0 {
		return false
	}
	return len(p.waiters) < p.maxQueueSize
}

func (p *Pool) notifyWaiterLocked() {
	if len(p.waiters) == 0 {
		return
	}
	waiter := p.waiters[0]
	p.waiters = p.waiters[1:]
	close(waiter)
}

func (p *Pool) removeWaiterLocked(waiter chan struct{}) bool {
	for i, w := range p.waiters {
		if w != waiter {
			continue
		}
		p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
		return true
	}
	return false
}

func (p *Pool) drainWaitersLocked() {
	for _, waiter := range p.waiters {
		close(waiter)
	}
	p.waiters = nil
}
