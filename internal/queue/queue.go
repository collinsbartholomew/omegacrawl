package queue

import (
	"container/heap"
	"context"
	"sync"
)

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

// DefaultMaxQueueSize is the default maximum number of items a queue holds.
const DefaultMaxQueueSize = 100000

// PriorityQueue is an in-memory priority queue that dequeues the lowest-depth item first.
type PriorityQueue struct {
	items   []*URLItem
	seen    map[string]bool
	mu      sync.Mutex
	maxSize int
}

// NewPriorityQueue creates a PriorityQueue with the default max queue size.
func NewPriorityQueue() *PriorityQueue {
	return NewPriorityQueueWithMaxSize(DefaultMaxQueueSize)
}

// NewPriorityQueueWithMaxSize creates a PriorityQueue with the given maxSize.
func NewPriorityQueueWithMaxSize(maxSize int) *PriorityQueue {
	pq := &PriorityQueue{
		items:   make([]*URLItem, 0),
		seen:    make(map[string]bool),
		maxSize: maxSize,
	}
	heap.Init(pq)
	return pq
}

// Len returns the number of items in the queue.
func (pq *PriorityQueue) Len() int { return len(pq.items) }

// Less reports whether the item at i has a lower depth than the item at j.
func (pq *PriorityQueue) Less(i, j int) bool {
	return pq.items[i].Depth < pq.items[j].Depth
}

// Swap exchanges the items at indices i and j and updates their Index fields.
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].Index = i
	pq.items[j].Index = j
}

// Push appends an item to the queue and sets its Index.
func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*URLItem)
	item.Index = len(pq.items)
	pq.items = append(pq.items, item)
}

// Pop removes and returns the last item in the queue.
func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.Index = -1
	pq.items = old[0 : n-1]
	return item
}

// MaxSize returns the configured maximum queue size.
func (q *PriorityQueue) MaxSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.maxSize
}

// SetMaxSize updates the maximum queue size.
func (q *PriorityQueue) SetMaxSize(maxSize int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maxSize = maxSize
}

// PushURL enqueues url at depth unless it was already seen or the queue is full.
func (q *PriorityQueue) PushURL(url string, depth int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.seen[url] {
		return false
	}
	if len(q.items) >= q.maxSize {
		return false
	}

	q.seen[url] = true
	item := &URLItem{URL: url, Depth: depth}
	heap.Push(q, item)
	return true
}

// PopURL removes and returns the lowest-depth item from the queue.
func (q *PriorityQueue) PopURL() (URLItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return URLItem{}, false
	}

	item := heap.Pop(q).(*URLItem)
	return *item, true
}

// Size returns the number of items in the queue.
func (q *PriorityQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// HasSeen reports whether the URL has been seen.
func (q *PriorityQueue) HasSeen(url string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.seen[url]
}

// MarkSeen records the URL as seen.
func (q *PriorityQueue) MarkSeen(url string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seen[url] = true
}

// Items returns a copy of all queued items.
func (q *PriorityQueue) Items() []URLItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]URLItem, len(q.items))
	for i, item := range q.items {
		result[i] = *item
	}
	return result
}

// Snapshot returns a consistent snapshot of the queue contents and visited set.
func (q *PriorityQueue) Snapshot() ([]URLItem, map[string]bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]URLItem, len(q.items))
	for i, item := range q.items {
		result[i] = *item
	}
	visited := make(map[string]bool, len(q.seen))
	for k, v := range q.seen {
		visited[k] = v
	}
	return result, visited
}

// AllVisited returns a copy of the seen URL set.
func (q *PriorityQueue) AllVisited() map[string]bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make(map[string]bool, len(q.seen))
	for k, v := range q.seen {
		result[k] = v
	}
	return result
}

// LoadFromCheckpoint replaces the queue contents with the given items and visited set.
func (q *PriorityQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = make([]*URLItem, len(items))
	q.seen = make(map[string]bool, len(visited))
	for k, v := range visited {
		q.seen[k] = v
	}
	for i := range items {
		item := items[i]
		q.items[i] = &item
	}
	heap.Init(q)
}

// Close is a no-op for PriorityQueue.
func (q *PriorityQueue) Close() error {
	return nil
}

// NewQueue builds a Queue for the given backend using the supplied connection
// strings and maxSize. Unknown backends fall back to an in-memory queue.
func NewQueue(ctx context.Context, backend, redisURL, pgDSN, kafkaURL string, maxSize int) (Queue, error) {
	switch backend {
	case "local":
		return NewPriorityQueueWithMaxSize(maxSize), nil
	case "redis":
		return NewRedisQueueWithSize(ctx, redisURL, maxSize)
	case "postgres":
		return NewPostgresQueueWithSize(ctx, pgDSN, maxSize)
	case "kafka":
		return NewKafkaQueueWithSize(ctx, kafkaURL, maxSize)
	default:
		return NewPriorityQueueWithMaxSize(maxSize), nil
	}
}
