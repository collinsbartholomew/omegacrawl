package util

import "sync"

// BoundedQueue is a thread-safe FIFO queue with a fixed capacity that drops
// the oldest item when full. Uses a ring buffer for O(1) push/pop.
type BoundedQueue struct {
	capacity int
	mu       sync.Mutex
	items    []interface{}
	head     int // index of oldest element
	tail     int // index where next element will be inserted
	size     int // current number of elements
}

// NewBoundedQueue creates a BoundedQueue with the given capacity, defaulting
// to 1000 if capacity is non-positive.
func NewBoundedQueue(capacity int) *BoundedQueue {
	if capacity <= 0 {
		capacity = 1000
	}
	return &BoundedQueue{
		capacity: capacity,
		items:    make([]interface{}, capacity),
	}
}

// Push appends item to the queue, discarding the oldest item if the queue is
// at capacity. O(1) operation.
func (q *BoundedQueue) Push(item interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size == q.capacity {
		// Queue is full, overwrite oldest (head) and advance head
		q.items[q.head] = item
		q.head = (q.head + 1) % q.capacity
		q.tail = (q.tail + 1) % q.capacity
	} else {
		// Queue has space
		q.items[q.tail] = item
		q.tail = (q.tail + 1) % q.capacity
		q.size++
	}
}

// GetAll returns a copy of the items currently in the queue in FIFO order.
func (q *BoundedQueue) GetAll() []interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size == 0 {
		return nil
	}

	result := make([]interface{}, q.size)
	for i := 0; i < q.size; i++ {
		idx := (q.head + i) % q.capacity
		result[i] = q.items[idx]
	}
	return result
}

// Len returns the number of items in the queue.
func (q *BoundedQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size
}

// Clear removes all items from the queue.
func (q *BoundedQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.head = 0
	q.tail = 0
	q.size = 0
	// Clear references to help GC
	for i := range q.items {
		q.items[i] = nil
	}
}
