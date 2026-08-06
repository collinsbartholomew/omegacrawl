package coordinator

import (
	"context"
	"time"
)

// BackendType represents the type of coordination backend.
type BackendType string

const (
	// BackendNone means no coordination (single worker mode).
	BackendNone BackendType = "none"
	// BackendFile uses a file-based lock for coordination (single host).
	BackendFile BackendType = "file"
	// BackendRedis uses Redis for distributed coordination.
	BackendRedis BackendType = "redis"
	// BackendEtcd uses etcd for distributed coordination.
	BackendEtcd BackendType = "etcd"
)

// Coordinator manages distributed worker coordination including leader election,
// worker registration, and task distribution.
type Coordinator interface {
	// Start begins the coordination loop.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the coordinator.
	Stop() error

	// RegisterWorker registers this worker with the coordinator.
	RegisterWorker(ctx context.Context, worker *WorkerInfo) error

	// DeregisterWorker removes this worker from the coordinator.
	DeregisterWorker(ctx context.Context, workerID string) error

	// IsLeader returns true if this worker is the elected leader.
	IsLeader() bool

	// LeaderID returns the current leader's worker ID.
	LeaderID() string

	// GetWorkers returns all registered workers.
	GetWorkers(ctx context.Context) ([]*WorkerInfo, error)

	// AcquireLock attempts to acquire a distributed lock for the given key.
	// Returns true if acquired, false otherwise.
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// ReleaseLock releases a previously acquired lock.
	ReleaseLock(ctx context.Context, key string) error

	// WatchLeaderChange registers a callback for leader changes.
	WatchLeaderChange(ctx context.Context, callback func(leaderID string)) error
}

// WorkerInfo represents information about a worker node.
type WorkerInfo struct {
	ID             string            `json:"id"`
	Hostname       string            `json:"hostname"`
	PID            int               `json:"pid"`
	StartTime      time.Time         `json:"start_time"`
	Capabilities   []string          `json:"capabilities"` // e.g., "crawl", "api", "dashboard"
	Metadata       map[string]string `json:"metadata"`
	LastHeartbeat  time.Time         `json:"last_heartbeat"`
}

// WorkerStatus represents the current status of a worker.
type WorkerStatus string

const (
	WorkerStatusHealthy   WorkerStatus = "healthy"
	WorkerStatusUnhealthy WorkerStatus = "unhealthy"
	WorkerStatusStopped   WorkerStatus = "stopped"
)

// Config holds the coordinator configuration.
type Config struct {
	Backend       BackendType   `json:"backend"`
	RedisURL      string        `json:"redis_url"`
	EtcdEndpoints []string      `json:"etcd_endpoints"`
	LockDir       string        `json:"lock_dir"` // For file backend
	WorkerID      string        `json:"worker_id"`
	HeartbeatTTL  time.Duration `json:"heartbeat_ttl"`
	LockTTL       time.Duration `json:"lock_ttl"`
}

// DefaultConfig returns a default coordinator configuration.
func DefaultConfig() *Config {
	return &Config{
		Backend:      BackendNone,
		LockDir:      "/tmp/clone-coordinator",
		HeartbeatTTL: 30 * time.Second,
		LockTTL:      10 * time.Second,
	}
}