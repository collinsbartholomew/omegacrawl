package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/user/clone/internal/util"
)

type RedisQueue struct {
	client    *redis.Client
	key       string
	maxSize   int
	parentCtx context.Context
}

func NewRedisQueue(ctx context.Context, redisURL string) (*RedisQueue, error) {
	return NewRedisQueueWithSize(ctx, redisURL, DefaultMaxQueueSize)
}

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

func (q *RedisQueue) Size() int {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	n, err := q.client.LLen(opCtx, q.key).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

func (q *RedisQueue) HasSeen(url string) bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	ok, err := q.client.SIsMember(opCtx, q.key+":seen", url).Result()
	if err != nil {
		return false
	}
	return ok
}

func (q *RedisQueue) MarkSeen(url string) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	_ = q.client.SAdd(opCtx, q.key+":seen", url).Err()
}

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

func (q *RedisQueue) Close() error {
	return q.client.Close()
}