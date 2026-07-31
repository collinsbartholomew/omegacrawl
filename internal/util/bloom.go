package util

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

// BloomFilter is a thread-safe bloom filter used for content-hash
// deduplication. Unlike an LRU set, it never evicts entries, which makes
// it suitable for deduplicating large numbers of content hashes without
// re-downloading previously seen resources.
type BloomFilter struct {
	filter *bloom.BloomFilter
	mu     sync.RWMutex
}

// NewBloomFilter creates a bloom filter sized for the expected number of
// distinct items at the given false-positive rate.
func NewBloomFilter(expectedItems uint, falsePositiveRate float64) *BloomFilter {
	return &BloomFilter{
		filter: bloom.NewWithEstimates(expectedItems, falsePositiveRate),
	}
}

// Add inserts key into the filter.
func (b *BloomFilter) Add(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.filter.AddString(key)
}

// HasSeen reports whether key has likely been added. False positives are
// possible at the configured rate; false negatives are not.
func (b *BloomFilter) HasSeen(key string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.filter.TestString(key)
}

// AddIfAbsent adds key and reports whether it was not previously present.
func (b *BloomFilter) AddIfAbsent(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.filter.TestString(key) {
		return false
	}
	b.filter.AddString(key)
	return true
}

// Len returns an estimate of the number of distinct items added.
func (b *BloomFilter) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return int(b.filter.ApproximatedSize())
}
