package util

import (
	"sync"
)

type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

type LRUSet struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*lruNode
	head     *lruNode
	tail     *lruNode
}

func NewLRUSet(capacity int) *LRUSet {
	if capacity <= 0 {
		capacity = 1000
	}
	s := &LRUSet{
		capacity: capacity,
		items:    make(map[string]*lruNode, capacity),
	}
	s.head = &lruNode{}
	s.tail = &lruNode{}
	s.head.next = s.tail
	s.tail.prev = s.head
	return s
}

func (s *LRUSet) Add(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node, ok := s.items[key]; ok {
		s.moveToHead(node)
		return
	}

	node := &lruNode{key: key}
	s.items[key] = node
	s.addToHead(node)

	if len(s.items) > s.capacity {
		s.removeLRU()
	}
}

func (s *LRUSet) Contains(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.items[key]
	return ok
}

func (s *LRUSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *LRUSet) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]*lruNode, s.capacity)
	s.head.next = s.tail
	s.tail.prev = s.head
}

func (s *LRUSet) addToHead(node *lruNode) {
	node.prev = s.head
	node.next = s.head.next
	s.head.next.prev = node
	s.head.next = node
}

func (s *LRUSet) removeNode(node *lruNode) {
	prev := node.prev
	next := node.next
	prev.next = next
	next.prev = prev
}

func (s *LRUSet) moveToHead(node *lruNode) {
	s.removeNode(node)
	s.addToHead(node)
}

func (s *LRUSet) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0, len(s.items))
	for k := range s.items {
		keys = append(keys, k)
	}
	return keys
}

func (s *LRUSet) removeLRU() {
	node := s.tail.prev
	if node == s.head {
		return
	}
	s.removeNode(node)
	delete(s.items, node.key)
}

type BoundedQueue struct {
	capacity int
	mu       sync.Mutex
	items    []interface{}
}

func NewBoundedQueue(capacity int) *BoundedQueue {
	if capacity <= 0 {
		capacity = 1000
	}
	return &BoundedQueue{
		capacity: capacity,
		items:    make([]interface{}, 0, capacity),
	}
}

func (q *BoundedQueue) Push(item interface{}) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.capacity {
		q.items = q.items[1:]
	}
	q.items = append(q.items, item)
}

func (q *BoundedQueue) GetAll() []interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]interface{}, len(q.items))
	copy(result, q.items)
	return result
}

func (q *BoundedQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *BoundedQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = q.items[:0]
}
