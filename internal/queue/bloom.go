package queue

import (
	"os"
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

// BloomDedup is a thread-safe URL deduplicator backed by a Bloom filter.
type BloomDedup struct {
	filter *bloom.BloomFilter
	mu     sync.RWMutex
}

// NewBloomDedup creates a BloomDedup sized for the expected number of items and false positive rate.
func NewBloomDedup(expectedItems uint, falsePositiveRate float64) *BloomDedup {
	return &BloomDedup{
		filter: bloom.NewWithEstimates(expectedItems, falsePositiveRate),
	}
}

// HasSeen reports whether the URL may have been added to the filter.
func (b *BloomDedup) HasSeen(url string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.filter.TestString(url)
}

// Add records the URL in the filter.
func (b *BloomDedup) Add(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.filter.AddString(url)
}

// Count returns the approximate number of items recorded in the filter.
func (b *BloomDedup) Count() uint32 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.filter.ApproximatedSize()
}

// SaveToFile writes the encoded filter to the file at path.
func (b *BloomDedup) SaveToFile(path string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := b.filter.GobEncode()
	if err != nil {
		return err
	}

	_, err = file.Write(data)
	return err
}

// LoadFromFile replaces the filter contents with the encoded filter read from path.
func (b *BloomDedup) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.filter = bloom.New(1000, 3) // placeholder, will be replaced by GobDecode
	return b.filter.GobDecode(data)
}
