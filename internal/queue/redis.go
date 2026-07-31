package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

// RedisQueue is a Redis-backed queue using a list, hash, and set for storage.
type RedisQueue struct {
	client    *redis.Client
	key       string
	maxSize   int
	parentCtx context.Context
}

// NewRedisQueue creates a RedisQueue using the default max queue size.
func NewRedisQueue(ctx context.Context, redisURL string) (*RedisQueue, error) {
	return NewRedisQueueWithSize(ctx, redisURL, DefaultMaxQueueSize)
}

// NewRedisQueueWithSize connects to Redis with the given maxSize and verifies
// the connection with a ping.
func NewRedisQueueWithSize(ctx context.Context, redisURL string, maxSize int) (*RedisQueue, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}
	client := redis.NewClient(opts)
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &RedisQueue{
		client:    client,
		key:       "crawl:queue",
		maxSize:   maxSize,
		parentCtx: ctx,
	}, nil
}

var pushScript = redis.NewScript(`
	local key = KEYS[1]
	local itemsKey = KEYS[2]
	local seenKey = KEYS[3]
	local url = ARGV[1]
	local data = ARGV[2]
	local maxSize = tonumber(ARGV[3])

	if redis.call("SISMEMBER", seenKey, url) == 1 then
		return 0
	end
	if redis.call("LLEN", key) >= maxSize then
		return 0
	end
	redis.call("RPUSH", key, data)
	redis.call("HSET", itemsKey, url, data)
	redis.call("SADD", seenKey, url)
	return 1
`)

var popScript = redis.NewScript(`
	local key = KEYS[1]
	local itemsKey = KEYS[2]
	local data = redis.call("LPOP", key)
	if not data then
		return nil
	end
	local ok, item = pcall(cjson.decode, data)
	if ok and item.url then
		redis.call("HDEL", itemsKey, item.url)
	end
	return data
`)

// PushURL atomically enqueues url at depth unless it was already seen or the queue is full.
func (q *RedisQueue) PushURL(url string, depth int) bool {
	item := URLItem{URL: url, Depth: depth}
	data, err := json.Marshal(item)
	if err != nil {
		return false
	}
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	res, err := pushScript.Run(opCtx, q.client, []string{q.key, q.key + ":items", q.key + ":seen"}, url, string(data), q.maxSize).Result()
	if err != nil {
		return false
	}
	return res.(int64) == 1
}

// PopURL removes and returns the next item from the queue.
func (q *RedisQueue) PopURL() (URLItem, bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	val, err := popScript.Run(opCtx, q.client, []string{q.key, q.key + ":items"}).Result()
	if err != nil || val == nil {
		return URLItem{}, false
	}
	valStr, ok := val.(string)
	if !ok || valStr == "" {
		return URLItem{}, false
	}
	var item URLItem
	if err := json.Unmarshal([]byte(valStr), &item); err != nil {
		return URLItem{}, false
	}
	return item, true
}

// Size returns the number of items in the queue.
func (q *RedisQueue) Size() int {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	n, err := q.client.LLen(opCtx, q.key).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

// HasSeen reports whether the URL is in the seen set.
func (q *RedisQueue) HasSeen(url string) bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	ok, err := q.client.SIsMember(opCtx, q.key+":seen", url).Result()
	if err != nil {
		return false
	}
	return ok
}

// MarkSeen adds the URL to the Redis seen set.
func (q *RedisQueue) MarkSeen(url string) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	if err := q.client.SAdd(opCtx, q.key+":seen", url).Err(); err != nil {
		util.LogError("failed to mark URL seen in redis", err, zap.String("url", url))
	}
}

// Items returns the items currently stored in the queue hash.
func (q *RedisQueue) Items() []URLItem {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	vals, err := q.client.HGetAll(opCtx, q.key+":items").Result()
	if err != nil {
		return nil
	}
	items := make([]URLItem, 0, len(vals))
	for _, v := range vals {
		var item URLItem
		if err := json.Unmarshal([]byte(v), &item); err == nil {
			items = append(items, item)
		}
	}
	return items
}

// AllVisited returns the set of URLs in the Redis seen set.
func (q *RedisQueue) AllVisited() map[string]bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	vals, err := q.client.SMembers(opCtx, q.key+":seen").Result()
	if err != nil {
		return nil
	}
	result := make(map[string]bool, len(vals))
	for _, v := range vals {
		result[v] = true
	}
	return result
}

// Snapshot returns a consistent snapshot of the queue contents and visited set.
func (q *RedisQueue) Snapshot() ([]URLItem, map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 10*time.Second)
	defer opCancel()
	// Use a Lua script for atomic snapshot
	script := redis.NewScript(`
		local items = redis.call('HGETALL', KEYS[1] .. ':items')
		local seen = redis.call('SMEMBERS', KEYS[1] .. ':seen')
		return {items, seen}
	`)
	result, err := script.Run(opCtx, q.client, []string{q.key}).Slice()
	if err != nil {
		// Fallback to non-atomic calls
		return q.Items(), q.AllVisited()
	}
	var items []URLItem
	if len(result) > 0 {
		if itemVals, ok := result[0].([]interface{}); ok {
			for _, v := range itemVals {
				var item URLItem
				if b, ok := v.(string); ok {
					if err := json.Unmarshal([]byte(b), &item); err == nil {
						items = append(items, item)
					}
				}
			}
		}
	}
	visited := make(map[string]bool)
	if len(result) > 1 {
		if seenVals, ok := result[1].([]interface{}); ok {
			for _, v := range seenVals {
				if s, ok := v.(string); ok {
					visited[s] = true
				}
			}
		}
	}
	return items, visited
}

// LoadFromCheckpoint clears the queue and reloads it from the given items and visited set.
func (q *RedisQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 10*time.Second)
	defer opCancel()
	pipe := q.client.Pipeline()
	pipe.Del(opCtx, q.key, q.key+":seen", q.key+":items")
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			continue
		}
		pipe.RPush(opCtx, q.key, string(data))
		pipe.HSet(opCtx, q.key+":items", item.URL, data)
	}
	for url := range visited {
		pipe.SAdd(opCtx, q.key+":seen", url)
	}
	if _, err := pipe.Exec(opCtx); err != nil {
		util.LogError("redis checkpoint load failed", err)
	}
}

// Close closes the underlying Redis client.
func (q *RedisQueue) Close() error {
	return q.client.Close()
}
