package queue

import "sync"

// Queue is the interface implemented by all crawl queue backends.
type Queue interface {
	PushURL(url string, depth int) bool
	PopURL() (URLItem, bool)
	Size() int
	HasSeen(url string) bool
	MarkSeen(url string)
	Items() []URLItem
	AllVisited() map[string]bool
	// Snapshot returns a consistent snapshot of the queue contents and visited set.
	Snapshot() ([]URLItem, map[string]bool)
	LoadFromCheckpoint(items []URLItem, visited map[string]bool)
	Close() error
}

// URLItem represents a single queued URL with its crawl depth.
type URLItem struct {
	URL   string
	Depth int
	Index int
}

// PriorityQueue is an in-memory priority queue that dequeues the lowest-depth item first.
type PriorityQueue struct {
	items   []*URLItem
	seen    map[string]bool
	mu      sync.Mutex
	maxSize int
}

// DefaultMaxQueueSize is the default maximum number of items a queue holds.
const DefaultMaxQueueSize = 100000
