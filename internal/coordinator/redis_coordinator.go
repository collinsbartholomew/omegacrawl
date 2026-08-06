package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCoordinator implements Coordinator using Redis for distributed coordination.
type RedisCoordinator struct {
	cfg      *Config
	client   *redis.Client
	worker   *WorkerInfo
	mu       sync.RWMutex
	lockKeys map[string]bool
	leaderID string
	workers  map[string]*WorkerInfo
	stopCh   chan struct{}
	wg       sync.WaitGroup
	leaderCB func(string)
	pubsub   *redis.PubSub
}

// NewRedisCoordinator creates a new Redis-based coordinator.
func NewRedisCoordinator(cfg *Config, worker *WorkerInfo) (*RedisCoordinator, error) {
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("redis_url required for Redis coordinator")
	}

	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	rc := &RedisCoordinator{
		cfg:      cfg,
		client:   client,
		worker:   worker,
		lockKeys: make(map[string]bool),
		workers:  make(map[string]*WorkerInfo),
		stopCh:   make(chan struct{}),
	}

	// Register self
	rc.workers[worker.ID] = worker

	return rc, nil
}

// Start begins the coordination loop.
func (rc *RedisCoordinator) Start(ctx context.Context) error {
	rc.wg.Add(1)
	go rc.heartbeatLoop(ctx)

	rc.wg.Add(1)
	go rc.leaderElectionLoop(ctx)

	rc.wg.Add(1)
	go rc.workerWatcher(ctx)

	return nil
}

// Stop gracefully shuts down the coordinator.
func (rc *RedisCoordinator) Stop() error {
	close(rc.stopCh)
	rc.wg.Wait()

	// Release all locks
	rc.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for key := range rc.lockKeys {
		rc.releaseLockInternal(ctx, key)
	}
	rc.lockKeys = nil

	// Deregister self
	deregisterCtx, deregisterCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer deregisterCancel()
	rc.deregisterWorkerInternal(deregisterCtx, rc.worker.ID)
	rc.mu.Unlock()

	if rc.pubsub != nil {
		rc.pubsub.Close()
	}
	rc.client.Close()

	return nil
}

// RegisterWorker registers this worker with the coordinator.
func (rc *RedisCoordinator) RegisterWorker(ctx context.Context, worker *WorkerInfo) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.workers[worker.ID] = worker
	return rc.persistWorkers(ctx)
}

// DeregisterWorker removes this worker from the coordinator.
func (rc *RedisCoordinator) DeregisterWorker(ctx context.Context, workerID string) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	delete(rc.workers, workerID)
	return rc.persistWorkers(ctx)
}

// IsLeader returns true if this worker is the elected leader.
func (rc *RedisCoordinator) IsLeader() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.leaderID == rc.worker.ID
}

// LeaderID returns the current leader's worker ID.
func (rc *RedisCoordinator) LeaderID() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.leaderID
}

// GetWorkers returns all registered workers.
func (rc *RedisCoordinator) GetWorkers(ctx context.Context) ([]*WorkerInfo, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	workers := make([]*WorkerInfo, 0, len(rc.workers))
	for _, w := range rc.workers {
		workers = append(workers, w)
	}
	return workers, nil
}

// AcquireLock attempts to acquire a distributed lock for the given key.
func (rc *RedisCoordinator) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	lockKey := "coordinator:locks:" + key

	rc.mu.Lock()
	if _, ok := rc.lockKeys[key]; ok {
		rc.mu.Unlock()
		return true, nil
	}
	rc.mu.Unlock()

	// Use SET NX EX for atomic lock acquisition
	acquired, err := rc.client.SetNX(ctx, lockKey, rc.worker.ID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		rc.mu.Lock()
		rc.lockKeys[key] = true
		rc.mu.Unlock()

		// Start lock renewal
		rc.wg.Add(1)
		go rc.renewLock(ctx, key, ttl)

		return true, nil
	}

	return false, nil
}

// ReleaseLock releases a previously acquired lock.
func (rc *RedisCoordinator) ReleaseLock(ctx context.Context, key string) error {
	rc.mu.Lock()
	if _, ok := rc.lockKeys[key]; !ok {
		rc.mu.Unlock()
		return nil
	}
	delete(rc.lockKeys, key)
	rc.mu.Unlock()

	return rc.releaseLockInternal(ctx, key)
}

// releaseLockInternal releases a lock without mutex (caller must hold lock).
func (rc *RedisCoordinator) releaseLockInternal(ctx context.Context, key string) error {
	lockKey := "coordinator:locks:" + key

	// Use Lua script to atomically check and delete
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)

	_, err := script.Run(ctx, rc.client, []string{lockKey}, rc.worker.ID).Result()
	return err
}

// WatchLeaderChange registers a callback for leader changes.
func (rc *RedisCoordinator) WatchLeaderChange(ctx context.Context, callback func(leaderID string)) error {
	rc.mu.Lock()
	rc.leaderCB = callback
	rc.mu.Unlock()
	return nil
}

// heartbeatLoop periodically updates worker heartbeat.
func (rc *RedisCoordinator) heartbeatLoop(ctx context.Context) {
	defer rc.wg.Done()

	ticker := time.NewTicker(rc.cfg.HeartbeatTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case <-ticker.C:
			rc.updateHeartbeat(ctx)
		}
	}
}

// updateHeartbeat updates this worker's heartbeat in Redis.
func (rc *RedisCoordinator) updateHeartbeat(ctx context.Context) {
	workerKey := "coordinator:workers:" + rc.worker.ID
	rc.worker.LastHeartbeat = time.Now()

	data, err := json.Marshal(rc.worker)
	if err != nil {
		return
	}

	rc.client.Set(ctx, workerKey, data, rc.cfg.HeartbeatTTL*2)
}

// leaderElectionLoop runs the leader election process.
func (rc *RedisCoordinator) leaderElectionLoop(ctx context.Context) {
	defer rc.wg.Done()

	ticker := time.NewTicker(rc.cfg.LockTTL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case <-ticker.C:
			rc.runLeaderElection(ctx)
		}
	}
}

// runLeaderElection attempts to become the leader.
func (rc *RedisCoordinator) runLeaderElection(ctx context.Context) {
	leaderKey := "coordinator:leader"

	// Try to acquire leader lock
	acquired, err := rc.client.SetNX(ctx, leaderKey, rc.worker.ID, rc.cfg.LockTTL).Result()
	if err != nil {
		return
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if acquired {
		if rc.leaderID != rc.worker.ID {
			rc.leaderID = rc.worker.ID
			if rc.leaderCB != nil {
				go rc.leaderCB(rc.worker.ID)
			}
		}
		// Publish leader change
		rc.client.Publish(ctx, "coordinator:leader:changes", rc.worker.ID)
	} else {
		// Read current leader
		leaderID, err := rc.client.Get(ctx, leaderKey).Result()
		if err == nil && rc.leaderID != leaderID {
			rc.leaderID = leaderID
			if rc.leaderCB != nil {
				go rc.leaderCB(leaderID)
			}
		}
	}
}

// workerWatcher watches for worker changes.
func (rc *RedisCoordinator) workerWatcher(ctx context.Context) {
	defer rc.wg.Done()

	rc.pubsub = rc.client.Subscribe(ctx, "coordinator:worker:changes")
	defer rc.pubsub.Close()

	ch := rc.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case msg := <-ch:
			var worker WorkerInfo
			if err := json.Unmarshal([]byte(msg.Payload), &worker); err == nil {
				rc.mu.Lock()
				rc.workers[worker.ID] = &worker
				rc.mu.Unlock()
			}
		}
	}
}

// renewLock periodically renews a lock.
func (rc *RedisCoordinator) renewLock(ctx context.Context, key string, ttl time.Duration) {
	defer rc.wg.Done()

	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case <-ticker.C:
			rc.mu.RLock()
			_, ok := rc.lockKeys[key]
			rc.mu.RUnlock()
			if !ok {
				return
			}
			lockKey := "coordinator:locks:" + key
			// Use Lua to atomically renew if we own it
			script := redis.NewScript(`
				if redis.call("GET", KEYS[1]) == ARGV[1] then
					return redis.call("EXPIRE", KEYS[1], ARGV[2])
				else
					return 0
				end
			`)
			script.Run(ctx, rc.client, []string{lockKey}, rc.worker.ID, int(ttl.Seconds()))
		}
	}
}

// persistWorkers writes the worker registry to Redis.
func (rc *RedisCoordinator) persistWorkers(ctx context.Context) error {
	data, err := json.Marshal(rc.worker)
	if err != nil {
		return err
	}
	workerKey := "coordinator:workers:" + rc.worker.ID
	return rc.client.Set(ctx, workerKey, data, rc.cfg.HeartbeatTTL*2).Err()
}

// deregisterWorkerInternal removes a worker from Redis.
func (rc *RedisCoordinator) deregisterWorkerInternal(ctx context.Context, workerID string) error {
	workerKey := "coordinator:workers:" + workerID
	return rc.client.Del(ctx, workerKey).Err()
}