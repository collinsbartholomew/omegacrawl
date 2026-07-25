package queue

import (
	"os"
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

type BloomDedup struct {
	filter *bloom.BloomFilter
	mu     sync.RWMutex
}

func NewBloomDedup(expectedItems uint, falsePositiveRate float64) *BloomDedup {
	return &BloomDedup{
		filter: bloom.NewWithEstimates(expectedItems, falsePositiveRate),
	}
}

func (b *BloomDedup) HasSeen(url string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.filter.TestString(url)
}

func (b *BloomDedup) Add(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.filter.AddString(url)
}

func (b *BloomDedup) Count() uint32 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.filter.ApproximatedSize()
}

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

func (b *BloomDedup) LoadFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read all data
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.filter = bloom.New(1000, 3) // placeholder, will be replaced by GobDecode
	return b.filter.GobDecode(data)
}