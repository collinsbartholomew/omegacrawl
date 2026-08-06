package coordinator

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// NewCoordinator creates a new Coordinator based on the configuration.
func NewCoordinator(cfg *Config) (Coordinator, error) {
	hostname, _ := os.Hostname()
	worker := &WorkerInfo{
		ID:        cfg.WorkerID,
		Hostname:  hostname,
		PID:       os.Getpid(),
		StartTime: time.Now(),
		Capabilities: []string{"crawl"},
		Metadata:  make(map[string]string),
	}

	if worker.ID == "" {
		worker.ID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}

	switch cfg.Backend {
	case BackendNone:
		return &NoOpCoordinator{worker: worker}, nil
	case BackendFile:
		return NewFileCoordinator(cfg, worker)
	case BackendRedis:
		return NewRedisCoordinator(cfg, worker)
	default:
		return nil, fmt.Errorf("unsupported coordinator backend: %s", cfg.Backend)
	}
}

// NoOpCoordinator is a no-op coordinator for single-worker mode.
type NoOpCoordinator struct {
	worker *WorkerInfo
}

func (n *NoOpCoordinator) Start(ctx context.Context) error {
	util.LogInfo("coordinator started in standalone mode", zap.String("worker_id", n.worker.ID))
	return nil
}

func (n *NoOpCoordinator) Stop() error {
	util.LogInfo("coordinator stopped")
	return nil
}

func (n *NoOpCoordinator) RegisterWorker(ctx context.Context, worker *WorkerInfo) error {
	return nil
}

func (n *NoOpCoordinator) DeregisterWorker(ctx context.Context, workerID string) error {
	return nil
}

func (n *NoOpCoordinator) IsLeader() bool {
	return true // Always leader in standalone mode
}

func (n *NoOpCoordinator) LeaderID() string {
	return n.worker.ID
}

func (n *NoOpCoordinator) GetWorkers(ctx context.Context) ([]*WorkerInfo, error) {
	return []*WorkerInfo{n.worker}, nil
}

func (n *NoOpCoordinator) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return true, nil // Always acquire in standalone mode
}

func (n *NoOpCoordinator) ReleaseLock(ctx context.Context, key string) error {
	return nil
}

func (n *NoOpCoordinator) WatchLeaderChange(ctx context.Context, callback func(leaderID string)) error {
	// Call immediately with our worker ID
	go callback(n.worker.ID)
	return nil
}

// WithCoordinator adds coordinator configuration to the crawler config.
func WithCoordinator(cfg *Config) *Config {
	if cfg.WorkerID == "" {
		hostname, _ := os.Hostname()
		cfg.WorkerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	return cfg
}

// DefaultCoordinatorConfig returns a default coordinator configuration.
func DefaultCoordinatorConfig() *Config {
	return DefaultConfig()
}