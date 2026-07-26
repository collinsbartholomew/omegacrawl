package sync

import (
	"runtime"
	"sync"
	"time"
)

func shardCount() uint32 {
	n := runtime.GOMAXPROCS(0)
	if n < 4 {
		n = 4
	}
	return uint32(n * 4)
}

type Shard[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]V
}

type ShardedMap[K comparable, V any] struct {
	shards    []Shard[K, V]
	hashFn    func(key K) uint32
	shardMask uint32
}

func NewShardedMap[K comparable, V any](opts ...func(*ShardedMap[K, V])) *ShardedMap[K, V] {
	count := shardCount()
	mask := count - 1
	sm := &ShardedMap[K, V]{
		shards:    make([]Shard[K, V], count),
		hashFn:    defaultHash[K],
		shardMask: mask,
	}
	for i := range sm.shards {
		sm.shards[i].items = make(map[K]V)
	}
	for _, opt := range opts {
		opt(sm)
	}
	return sm
}

func defaultHash[K comparable](key K) uint32 {
	s := interface{}(key).(string)
	var h uint32
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	return h
}

func WithHashFn[K comparable, V any](fn func(K) uint32) func(*ShardedMap[K, V]) {
	return func(sm *ShardedMap[K, V]) {
		sm.hashFn = fn
	}
}

func (sm *ShardedMap[K, V]) getShard(key K) *Shard[K, V] {
	return &sm.shards[sm.hashFn(key)&sm.shardMask]
}

func (sm *ShardedMap[K, V]) Get(key K) (V, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	v, ok := shard.items[key]
	shard.mu.RUnlock()
	return v, ok
}

func (sm *ShardedMap[K, V]) Set(key K, val V) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	shard.items[key] = val
	shard.mu.Unlock()
}

func (sm *ShardedMap[K, V]) Delete(key K) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

func (sm *ShardedMap[K, V]) Has(key K) bool {
	shard := sm.getShard(key)
	shard.mu.RLock()
	_, ok := shard.items[key]
	shard.mu.RUnlock()
	return ok
}

func (sm *ShardedMap[K, V]) GetOrSet(key K, val V) (actual V, loaded bool) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	if existing, ok := shard.items[key]; ok {
		shard.mu.Unlock()
		return existing, true
	}
	shard.items[key] = val
	shard.mu.Unlock()
	return val, false
}

func (sm *ShardedMap[K, V]) Len() int {
	total := 0
	for i := range sm.shards {
		sm.shards[i].mu.RLock()
		total += len(sm.shards[i].items)
		sm.shards[i].mu.RUnlock()
	}
	return total
}

func (sm *ShardedMap[K, V]) Range(fn func(key K, val V) bool) {
	for i := range sm.shards {
		sm.shards[i].mu.RLock()
		for k, v := range sm.shards[i].items {
			if !fn(k, v) {
				sm.shards[i].mu.RUnlock()
				return
			}
		}
		sm.shards[i].mu.RUnlock()
	}
}

func (sm *ShardedMap[K, V]) Clear() {
	for i := range sm.shards {
		sm.shards[i].mu.Lock()
		sm.shards[i].items = make(map[K]V)
		sm.shards[i].mu.Unlock()
	}
}

type ShardedCounter struct {
	shards []Shard[string, int64]
	mask   uint32
}

func NewShardedCounter() *ShardedCounter {
	count := shardCount()
	return &ShardedCounter{
		shards: make([]Shard[string, int64], count),
		mask:   count - 1,
	}
}

func (sc *ShardedCounter) getShard(key string) *Shard[string, int64] {
	return &sc.shards[defaultHash[string](key)&sc.mask]
}

func (sc *ShardedCounter) Inc(key string, delta int64) int64 {
	shard := sc.getShard(key)
	shard.mu.Lock()
	shard.items[key] += delta
	newVal := shard.items[key]
	shard.mu.Unlock()
	return newVal
}

func (sc *ShardedCounter) Get(key string) int64 {
	shard := sc.getShard(key)
	shard.mu.RLock()
	v := shard.items[key]
	shard.mu.RUnlock()
	return v
}

func (sc *ShardedCounter) Reset(key string) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

type ShardedTimeMap struct {
	shards []Shard[string, time.Time]
	mask   uint32
}

func NewShardedTimeMap() *ShardedTimeMap {
	count := shardCount()
	sm := &ShardedTimeMap{
		shards: make([]Shard[string, time.Time], count),
		mask:   count - 1,
	}
	for i := range sm.shards {
		sm.shards[i].items = make(map[string]time.Time)
	}
	return sm
}

func (stm *ShardedTimeMap) getShard(key string) *Shard[string, time.Time] {
	return &stm.shards[defaultHash[string](key)&stm.mask]
}

func (stm *ShardedTimeMap) Set(key string, t time.Time) {
	shard := stm.getShard(key)
	shard.mu.Lock()
	shard.items[key] = t
	shard.mu.Unlock()
}

func (stm *ShardedTimeMap) Get(key string) (time.Time, bool) {
	shard := stm.getShard(key)
	shard.mu.RLock()
	t, ok := shard.items[key]
	shard.mu.RUnlock()
	return t, ok
}

func (stm *ShardedTimeMap) Delete(key string) {
	shard := stm.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

func (stm *ShardedTimeMap) Range(fn func(key string, val time.Time) bool) {
	for i := range stm.shards {
		stm.shards[i].mu.RLock()
		for k, v := range stm.shards[i].items {
			if !fn(k, v) {
				stm.shards[i].mu.RUnlock()
				return
			}
		}
		stm.shards[i].mu.RUnlock()
	}
}
