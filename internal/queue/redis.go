package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	client  *redis.Client
	key     string
	maxSize int
}

func NewRedisQueue(redisURL string) (*RedisQueue, error) {
	return NewRedisQueueWithSize(redisURL, DefaultMaxQueueSize)
}

func NewRedisQueueWithSize(redisURL string, maxSize int) (*RedisQueue, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &RedisQueue{
		client:  client,
		key:     "crawl:queue",
		maxSize: maxSize,
	}, nil
}

func (q *RedisQueue) PushURL(url string, depth int) bool {
	if q.HasSeen(url) {
		return false
	}
	if q.Size() >= q.maxSize {
		return false
	}
	item := URLItem{URL: url, Depth: depth}
	data, _ := json.Marshal(item)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipe := q.client.Pipeline()
	pipe.RPush(ctx, q.key, string(data))
	pipe.HSet(ctx, q.key+":items", url, data)
	pipe.SAdd(ctx, q.key+":seen", url)
	_, err := pipe.Exec(ctx)
	return err == nil
}

func (q *RedisQueue) PopURL() (URLItem, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	val, err := q.client.LPop(ctx, q.key).Result()
	if err != nil || val == "" {
		return URLItem{}, false
	}
	var item URLItem
	if err := json.Unmarshal([]byte(val), &item); err != nil {
		return URLItem{}, false
	}
	return item, true
}

func (q *RedisQueue) Size() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := q.client.LLen(ctx, q.key).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

func (q *RedisQueue) HasSeen(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := q.client.SIsMember(ctx, q.key+":seen", url).Result()
	if err != nil {
		return false
	}
	return ok
}

func (q *RedisQueue) MarkSeen(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = q.client.SAdd(ctx, q.key+":seen", url).Err()
}

func (q *RedisQueue) Items() []URLItem {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vals, err := q.client.HGetAll(ctx, q.key+":items").Result()
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vals, err := q.client.SMembers(ctx, q.key+":seen").Result()
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = q.client.Del(ctx, q.key, q.key+":seen", q.key+":items")
	for _, item := range items {
		data, _ := json.Marshal(item)
		_ = q.client.RPush(ctx, q.key, string(data))
		itemData, _ := json.Marshal(item)
		_ = q.client.HSet(ctx, q.key+":items", item.URL, itemData)
	}
	for url := range visited {
		_ = q.client.SAdd(ctx, q.key+":seen", url)
	}
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}