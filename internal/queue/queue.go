package queue

import (
	"container/heap"
	"context"
	"sync"
)

type Queue interface {
	PushURL(url string, depth int) bool
	PopURL() (URLItem, bool)
	Size() int
	HasSeen(url string) bool
	MarkSeen(url string)
	Items() []URLItem
	AllVisited() map[string]bool
	LoadFromCheckpoint(items []URLItem, visited map[string]bool)
	Close() error
}

type URLItem struct {
	URL   string
	Depth int
	Index int
}

const DefaultMaxQueueSize = 100000

type PriorityQueue struct {
	items   []*URLItem
	seen    map[string]bool
	mu      sync.Mutex
	maxSize int
}

func NewPriorityQueue() *PriorityQueue {
	return NewPriorityQueueWithMaxSize(DefaultMaxQueueSize)
}

func NewPriorityQueueWithMaxSize(maxSize int) *PriorityQueue {
	pq := &PriorityQueue{
		items:   make([]*URLItem, 0),
		seen:    make(map[string]bool),
		maxSize: maxSize,
	}
	heap.Init(pq)
	return pq
}

func (pq *PriorityQueue) Len() int { return len(pq.items) }

func (pq *PriorityQueue) Less(i, j int) bool {
	return pq.items[i].Depth < pq.items[j].Depth
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].Index = i
	pq.items[j].Index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*URLItem)
	item.Index = len(pq.items)
	pq.items = append(pq.items, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.Index = -1
	pq.items = old[0 : n-1]
	return item
}

func (q *PriorityQueue) MaxSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.maxSize
}

func (q *PriorityQueue) SetMaxSize(maxSize int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maxSize = maxSize
}

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

func (q *PriorityQueue) PopURL() (URLItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return URLItem{}, false
	}

	item := heap.Pop(q).(*URLItem)
	return *item, true
}

func (q *PriorityQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *PriorityQueue) HasSeen(url string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.seen[url]
}

func (q *PriorityQueue) MarkSeen(url string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seen[url] = true
}

func (q *PriorityQueue) Items() []URLItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]URLItem, len(q.items))
	for i, item := range q.items {
		result[i] = *item
	}
	return result
}

func (q *PriorityQueue) AllVisited() map[string]bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make(map[string]bool)
	for k, v := range q.seen {
		result[k] = v
	}
	return result
}

func (q *PriorityQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = make([]*URLItem, len(items))
	q.seen = visited
	for i := range items {
		q.items[i] = &items[i]
	}
	heap.Init(q)
}

func (q *PriorityQueue) Close() error {
	return nil
}

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