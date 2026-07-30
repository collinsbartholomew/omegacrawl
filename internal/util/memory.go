package util

import (
	"sync"
	"sync/atomic"
)

type MemoryBudget struct {
	maxBytes  int64
	usedBytes atomic.Int64
	cond      *sync.Cond
	mu        sync.Mutex
}

func NewMemoryBudget(maxBytes int64) *MemoryBudget {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024 * 1024
	}
	mb := &MemoryBudget{maxBytes: maxBytes}
	mb.cond = sync.NewCond(&mb.mu)
	return mb
}

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

func (mb *MemoryBudget) ReserveBlocking(n int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for mb.usedBytes.Load()+n > mb.maxBytes {
		mb.cond.Wait()
	}
	mb.usedBytes.Add(n)
}

func (mb *MemoryBudget) Release(n int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.usedBytes.Add(-n)
	mb.cond.Broadcast()
}

func (mb *MemoryBudget) Used() int64 {
	return mb.usedBytes.Load()
}

func (mb *MemoryBudget) Max() int64 {
	return mb.maxBytes
}

func (mb *MemoryBudget) Available() int64 {
	return mb.maxBytes - mb.usedBytes.Load()
}
