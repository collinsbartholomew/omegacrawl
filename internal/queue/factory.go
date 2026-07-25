package queue

import (
	"fmt"
	"strings"

	"github.com/user/clone/internal/config"
)

func NewQueueFromConfig(cfg *config.QueueConfig) (Queue, error) {
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
		return NewRedisQueue(cfg.RedisURL)
	case "postgres":
		if cfg.PgDSN == "" {
			return nil, fmt.Errorf("postgres backend requires pg_dsn")
		}
		return NewPostgresQueue(cfg.PgDSN)
	case "kafka":
		if cfg.KafkaURL == "" {
			return nil, fmt.Errorf("kafka backend requires kafka_url")
		}
		return NewKafkaQueue(cfg.KafkaURL)
	default:
		return nil, fmt.Errorf("unknown queue backend: %s", cfg.Backend)
	}
}