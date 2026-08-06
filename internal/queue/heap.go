package queue

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
