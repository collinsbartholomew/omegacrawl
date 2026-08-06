package util

import "sync"

type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

// LRUSet is a thread-safe set of strings with a fixed capacity that evicts
// the least recently used entry when full.
type LRUSet struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*lruNode
	head     *lruNode
	tail     *lruNode
}

// NewLRUSet creates an LRUSet with the given capacity, defaulting to 1000 if
// capacity is non-positive.
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

// Add inserts key into the set, marking it as most recently used and evicting
// the least recently used entry if the set is full.
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

// AddIfAbsent inserts key and reports whether it was not already present.
func (s *LRUSet) AddIfAbsent(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[key]; ok {
		return false
	}

	node := &lruNode{key: key}
	s.items[key] = node
	s.addToHead(node)

	if len(s.items) > s.capacity {
		s.removeLRU()
	}
	return true
}

// Contains reports whether key is in the set.
func (s *LRUSet) Contains(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.items[key]
	return ok
}

// Len returns the number of entries in the set.
func (s *LRUSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Clear removes all entries from the set.
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

// Keys returns a snapshot of all keys currently in the set.
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
