package queue

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/clone/internal/config"
)

// NewQueueFromConfig builds a Queue from the given config, selecting the backend
// based on cfg.Backend. A nil cfg returns an in-memory priority queue.
func NewQueueFromConfig(ctx context.Context, cfg *config.QueueConfig) (Queue, error) {
	if cfg == nil {
		return NewPriorityQueue(), nil
	}
	backend := strings.ToLower(cfg.Backend)
	switch backend {
	case "local", "":
		return NewPriorityQueue(), nil
	case "redis":
		if cfg.RedisURL == "" {
			return nil, fmt.Errorf("redis backend requires redis_url")
		}
		return NewRedisQueue(ctx, cfg.RedisURL)
	case "postgres":
		if cfg.PgDSN == "" {
			return nil, fmt.Errorf("postgres backend requires pg_dsn")
		}
		return NewPostgresQueue(ctx, cfg.PgDSN)
	case "kafka":
		if cfg.KafkaURL == "" {
			return nil, fmt.Errorf("kafka backend requires kafka_url")
		}
		return NewKafkaQueue(ctx, cfg.KafkaURL)
	default:
		return nil, fmt.Errorf("unknown queue backend: %s", cfg.Backend)
	}
}

// NewQueue builds a Queue for the given backend using the supplied connection
// strings and maxSize. Unknown backends fall back to an in-memory queue.
func NewQueue(ctx context.Context, backend, redisURL, pgDSN, kafkaURL string, maxSize int) (Queue, error) {
	switch backend {
	case "local":
		return NewPriorityQueueWithMaxSize(maxSize), nil
	case "redis":
		return NewRedisQueueWithSize(ctx, redisURL, maxSize)
	case "postgres":
		return NewPostgresQueueWithSize(ctx, pgDSN, maxSize)
	case "kafka":
		return NewKafkaQueueWithSize(ctx, kafkaURL, maxSize)
	default:
		return NewPriorityQueueWithMaxSize(maxSize), nil
	}
}
