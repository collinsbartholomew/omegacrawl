package util

import (
	"sync"
	"sync/atomic"
)

// MemoryBudget tracks and limits byte usage against a maximum budget,
// providing both non-blocking and blocking reservation.
type MemoryBudget struct {
	maxBytes  int64
	usedBytes atomic.Int64
	cond      *sync.Cond
	mu        sync.Mutex
}

// NewMemoryBudget creates a MemoryBudget with the given maximum, defaulting
// to 512 MiB if maxBytes is non-positive.
func NewMemoryBudget(maxBytes int64) *MemoryBudget {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024 * 1024
	}
	mb := &MemoryBudget{maxBytes: maxBytes}
	mb.cond = sync.NewCond(&mb.mu)
	return mb
}

// Reserve attempts to reserve n bytes, returning false immediately if doing
// so would exceed the budget.
func (mb *MemoryBudget) Reserve(n int64) bool {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	cur := mb.usedBytes.Load()
	if cur+n > mb.maxBytes {
		return false
	}
	mb.usedBytes.Store(cur + n)
	return true
}

// ReserveBlocking reserves n bytes, blocking until enough budget is
// available.
func (mb *MemoryBudget) ReserveBlocking(n int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for mb.usedBytes.Load()+n > mb.maxBytes {
		mb.cond.Wait()
	}
	mb.usedBytes.Add(n)
}

// Release returns n bytes to the budget and wakes any blocked waiters.
func (mb *MemoryBudget) Release(n int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.usedBytes.Add(-n)
	mb.cond.Broadcast()
}

// Used returns the number of bytes currently reserved.
func (mb *MemoryBudget) Used() int64 {
	return mb.usedBytes.Load()
}

// Max returns the configured maximum byte budget.
func (mb *MemoryBudget) Max() int64 {
	return mb.maxBytes
}

// Available returns the number of bytes still reservable within the budget.
func (mb *MemoryBudget) Available() int64 {
	return mb.maxBytes - mb.usedBytes.Load()
}
