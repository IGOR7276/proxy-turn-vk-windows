package core

import (
	"context"
	"sync"
	"time"
)

// allocateGateInterval — минимальный интервал между TURN Allocate попытками
// всех воркеров одного ядра. VK считает квоту на аккаунт/IP, поэтому без
// паузы одновременный старт десятков воркеров превращается в шторм 486.
const allocateGateInterval = 100 * time.Millisecond

// allocationGate разносит TURN Allocate во времени. Один gate на ядро, чтобы
// разные Core не блокировали друг друга.
type allocationGate struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newAllocationGate(interval time.Duration) *allocationGate {
	return &allocationGate{interval: interval}
}

// Wait блокирует до наступления слота и резервирует следующий.
func (g *allocationGate) Wait(ctx context.Context) error {
	if g == nil {
		return nil
	}

	g.mu.Lock()
	due := time.Now()
	if g.next.After(due) {
		due = g.next
	}
	g.next = due.Add(g.interval)
	g.mu.Unlock()

	wait := time.Until(due)
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
