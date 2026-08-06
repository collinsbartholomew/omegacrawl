package util

import "sync"

type lruValueNode struct {
	key   string
	value string
	prev  *lruValueNode
	next  *lruValueNode
}

// LRUCache is a thread-safe map of string keys to string values with a fixed
// capacity that evicts the least recently used entry when full.
type LRUCache struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*lruValueNode
	head     *lruValueNode
	tail     *lruValueNode
}

// NewLRUCache creates an LRUCache with the given capacity, defaulting to 1000
// if capacity is non-positive.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1000
	}
	s := &LRUCache{
		capacity: capacity,
		items:    make(map[string]*lruValueNode, capacity),
	}
	s.head = &lruValueNode{}
	s.tail = &lruValueNode{}
	s.head.next = s.tail
	s.tail.prev = s.head
	return s
}

// Get returns the value for key and marks it most recently used.
func (c *LRUCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.items[key]
	if !ok {
		return "", false
	}
	c.moveValueToHead(node)
	return node.value, true
}

// Put inserts or updates key with value, evicting the least recently used
// entry if the cache is full.
func (c *LRUCache) Put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		node.value = value
		c.moveValueToHead(node)
		return
	}
	node := &lruValueNode{key: key, value: value}
	c.items[key] = node
	c.addValueToHead(node)
	if len(c.items) > c.capacity {
		c.removeValueLRU()
	}
}

// PutIfAbsent inserts key only if absent. It returns the existing value (if
// any) and whether the key was newly added.
func (c *LRUCache) PutIfAbsent(key, value string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		c.moveValueToHead(node)
		return node.value, false
	}
	node := &lruValueNode{key: key, value: value}
	c.items[key] = node
	c.addValueToHead(node)
	if len(c.items) > c.capacity {
		c.removeValueLRU()
	}
	return value, true
}

func (c *LRUCache) addValueToHead(node *lruValueNode) {
	node.prev = c.head
	node.next = c.head.next
	c.head.next.prev = node
	c.head.next = node
}

func (c *LRUCache) removeValueNode(node *lruValueNode) {
	prev := node.prev
	next := node.next
	prev.next = next
	next.prev = prev
}

func (c *LRUCache) moveValueToHead(node *lruValueNode) {
	c.removeValueNode(node)
	c.addValueToHead(node)
}

func (c *LRUCache) removeValueLRU() {
	node := c.tail.prev
	if node == c.head {
		return
	}
	c.removeValueNode(node)
	delete(c.items, node.key)
}
