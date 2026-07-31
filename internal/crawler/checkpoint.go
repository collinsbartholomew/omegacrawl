package crawler

import (
	"encoding/gob"
	"os"
	"sync"
	"time"

	"github.com/user/clone/internal/queue"
)

// CheckpointData is the serialized state of a crawl saved to disk for resuming.
type CheckpointData struct {
	Queue         []queue.URLItem
	Visited       map[string]bool
	HostLastCrawl map[string]time.Time
	HostURLCount  map[string]int
	Timestamp     time.Time
}

// QueueItem is a URL queue item used within checkpoint data.
type QueueItem = queue.URLItem

// Checkpoint persists and restores crawl state to support resuming interrupted crawls.
type Checkpoint struct {
	data     *CheckpointData
	filePath string
	mu       sync.RWMutex
}

// NewCheckpoint creates a new Checkpoint backed by the given file path.
func NewCheckpoint(filePath string) *Checkpoint {
	return &Checkpoint{
		filePath: filePath,
		data: &CheckpointData{
			Visited:       make(map[string]bool),
			HostLastCrawl: make(map[string]time.Time),
			HostURLCount:  make(map[string]int),
		},
	}
}

// Save writes the current queue and crawl state to the checkpoint file atomically.
func (c *Checkpoint) Save(items []queue.URLItem, visited map[string]bool, hostLastCrawl map[string]time.Time, hostURLCount map[string]int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data.Queue = items
	c.data.Visited = visited
	c.data.HostLastCrawl = make(map[string]time.Time)
	for k, v := range hostLastCrawl {
		c.data.HostLastCrawl[k] = v
	}
	c.data.HostURLCount = make(map[string]int)
	for k, v := range hostURLCount {
		c.data.HostURLCount[k] = v
	}
	c.data.Timestamp = time.Now()

	// Write to temp file first, then atomic rename
	tmpPath := c.filePath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(c.data); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, c.filePath)
}

// Load reads the checkpoint file and returns the stored CheckpointData.
func (c *Checkpoint) Load() (*CheckpointData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	file, err := os.Open(c.filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	tmp := &CheckpointData{
		Visited:       make(map[string]bool),
		HostLastCrawl: make(map[string]time.Time),
		HostURLCount:  make(map[string]int),
	}
	err = decoder.Decode(tmp)
	if err != nil {
		return nil, err
	}
	c.data = tmp

	return c.data, nil
}

// Exists reports whether the checkpoint file already exists.
func (c *Checkpoint) Exists() bool {
	_, err := os.Stat(c.filePath)
	return err == nil
}

// Remove deletes the checkpoint file.
func (c *Checkpoint) Remove() error {
	return os.Remove(c.filePath)
}
