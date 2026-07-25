package crawler

import (
	"encoding/gob"
	"os"
	"sync"
	"time"

	"github.com/user/clone/internal/queue"
)

type CheckpointData struct {
	Queue         []queue.URLItem
	Visited       map[string]bool
	HostLastCrawl map[string]time.Time
	HostURLCount  map[string]int
	Timestamp     time.Time
}

type QueueItem = queue.URLItem

type Checkpoint struct {
	data     *CheckpointData
	filePath string
	mu       sync.RWMutex
}

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

func (c *Checkpoint) Save(q queue.Queue, hostLastCrawl map[string]time.Time, hostURLCount map[string]int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data.Queue = q.Items()
	c.data.Visited = q.AllVisited()
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
	file, err := os.Create(tmpPath)
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

func (c *Checkpoint) Load() (*CheckpointData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	file, err := os.Open(c.filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	c.data = &CheckpointData{
		Visited:       make(map[string]bool),
		HostLastCrawl: make(map[string]time.Time),
		HostURLCount:  make(map[string]int),
	}
	err = decoder.Decode(c.data)
	if err != nil {
		return nil, err
	}

	return c.data, nil
}

func (c *Checkpoint) Exists() bool {
	_, err := os.Stat(c.filePath)
	return err == nil
}

func (c *Checkpoint) Remove() error {
	return os.Remove(c.filePath)
}
