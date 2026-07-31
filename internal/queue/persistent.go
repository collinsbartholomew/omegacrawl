package queue

import (
	"container/heap"
	"encoding/json"
	"os"
	"sync"
)

// PersistentQueue is a PriorityQueue that can be persisted to and restored from a JSON file.
type PersistentQueue struct {
	PriorityQueue
	filePath string
	dirty    bool
	saveMu   sync.Mutex
}

// NewPersistentQueue creates a PersistentQueue with the given filePath and maxSize,
// restoring prior state from filePath if present.
func NewPersistentQueue(filePath string, maxSize int) *PersistentQueue {
	pq := &PersistentQueue{
		PriorityQueue: PriorityQueue{
			items:   make([]*URLItem, 0),
			seen:    make(map[string]bool),
			maxSize: maxSize,
		},
		filePath: filePath,
	}
	if filePath != "" {
		if data, err := os.ReadFile(filePath); err == nil {
			var state struct {
				Items []URLItem       `json:"items"`
				Seen  map[string]bool `json:"seen"`
			}
			if json.Unmarshal(data, &state) == nil {
				for i := range state.Items {
					state.Items[i].Index = i
				}
				pq.mu.Lock()
				pq.items = make([]*URLItem, len(state.Items))
				for i := range state.Items {
					pq.items[i] = &state.Items[i]
				}
				pq.seen = state.Seen
				pq.mu.Unlock()
				heap.Init(&pq.PriorityQueue)
			}
		}
	}
	return pq
}

// Close is a no-op for PersistentQueue.
func (q *PersistentQueue) Close() error {
	return nil
}

// Save writes the queue items and seen set to the configured file path.
func (q *PersistentQueue) Save() error {
	if q.filePath == "" {
		return nil
	}
	q.saveMu.Lock()
	defer q.saveMu.Unlock()

	q.mu.Lock()
	items := make([]URLItem, len(q.items))
	for i, item := range q.items {
		items[i] = *item
	}
	seen := make(map[string]bool)
	for k, v := range q.seen {
		seen[k] = v
	}
	q.mu.Unlock()

	state := struct {
		Items []URLItem       `json:"items"`
		Seen  map[string]bool `json:"seen"`
	}{Items: items, Seen: seen}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(q.filePath, data, 0600)
}
