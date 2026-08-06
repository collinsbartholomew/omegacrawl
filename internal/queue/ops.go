package queue

import "container/heap"

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
		q.items[i] = &items[i]
	}
	heap.Init(q)
}

// Close is a no-op for PriorityQueue.
func (q *PriorityQueue) Close() error {
	return nil
}
