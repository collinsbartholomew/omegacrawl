package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// FileCoordinator implements Coordinator using file-based locking.
// Suitable for single-host multi-process coordination.
type FileCoordinator struct {
	cfg       *Config
	worker    *WorkerInfo
	mu        sync.RWMutex
	lockFiles map[string]*os.File
	leaderID  string
	workers   map[string]*WorkerInfo
	stopCh    chan struct{}
	wg        sync.WaitGroup
	leaderCB  func(string)
}

// NewFileCoordinator creates a new file-based coordinator.
func NewFileCoordinator(cfg *Config, worker *WorkerInfo) (*FileCoordinator, error) {
	if cfg.LockDir == "" {
		cfg.LockDir = "/tmp/clone-coordinator"
	}
	if err := os.MkdirAll(cfg.LockDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock dir: %w", err)
	}

	fc := &FileCoordinator{
		cfg:       cfg,
		worker:    worker,
		lockFiles: make(map[string]*os.File),
		workers:   make(map[string]*WorkerInfo),
		stopCh:    make(chan struct{}),
	}

	// Register self
	fc.workers[worker.ID] = worker

	return fc, nil
}

// Start begins the coordination loop.
func (fc *FileCoordinator) Start(ctx context.Context) error {
	fc.wg.Add(1)
	go fc.heartbeatLoop(ctx)
	return nil
}

// Stop gracefully shuts down the coordinator.
func (fc *FileCoordinator) Stop() error {
	close(fc.stopCh)
	fc.wg.Wait()

	// Release all locks
	fc.mu.Lock()
	for key, f := range fc.lockFiles {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		os.Remove(filepath.Join(fc.cfg.LockDir, key+".lock"))
	}
	fc.lockFiles = nil

	// Deregister self
	delete(fc.workers, fc.worker.ID)
	fc.mu.Unlock()

	return nil
}

// RegisterWorker registers this worker with the coordinator.
func (fc *FileCoordinator) RegisterWorker(ctx context.Context, worker *WorkerInfo) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.workers[worker.ID] = worker
	return fc.persistWorkers()
}

// DeregisterWorker removes this worker from the coordinator.
func (fc *FileCoordinator) DeregisterWorker(ctx context.Context, workerID string) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	delete(fc.workers, workerID)
	return fc.persistWorkers()
}

// IsLeader returns true if this worker is the elected leader.
func (fc *FileCoordinator) IsLeader() bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.leaderID == fc.worker.ID
}

// LeaderID returns the current leader's worker ID.
func (fc *FileCoordinator) LeaderID() string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.leaderID
}

// GetWorkers returns all registered workers.
func (fc *FileCoordinator) GetWorkers(ctx context.Context) ([]*WorkerInfo, error) {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	workers := make([]*WorkerInfo, 0, len(fc.workers))
	for _, w := range fc.workers {
		workers = append(workers, w)
	}
	return workers, nil
}

// AcquireLock attempts to acquire a distributed lock for the given key.
func (fc *FileCoordinator) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	lockPath := filepath.Join(fc.cfg.LockDir, key+".lock")

	fc.mu.Lock()
	// Check if we already hold this lock
	if _, ok := fc.lockFiles[key]; ok {
		fc.mu.Unlock()
		return true, nil
	}
	fc.mu.Unlock()

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return false, nil
		}
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Write our worker ID to the lock file
	if err := f.Truncate(0); err != nil {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		return false, err
	}
	if _, err := f.WriteString(fc.worker.ID); err != nil {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		return false, err
	}
	f.Sync()

	fc.mu.Lock()
	fc.lockFiles[key] = f
	fc.mu.Unlock()

	// Start a goroutine to renew the lock
	fc.wg.Add(1)
	go fc.renewLock(ctx, key, f, ttl)

	return true, nil
}

// ReleaseLock releases a previously acquired lock.
func (fc *FileCoordinator) ReleaseLock(ctx context.Context, key string) error {
	fc.mu.Lock()
	f, ok := fc.lockFiles[key]
	if !ok {
		fc.mu.Unlock()
		return nil
	}
	delete(fc.lockFiles, key)
	fc.mu.Unlock()

	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	f.Close()

	lockPath := filepath.Join(fc.cfg.LockDir, key+".lock")
	os.Remove(lockPath)
	return nil
}

// WatchLeaderChange registers a callback for leader changes.
func (fc *FileCoordinator) WatchLeaderChange(ctx context.Context, callback func(leaderID string)) error {
	fc.mu.Lock()
	fc.leaderCB = callback
	fc.mu.Unlock()
	return nil
}

// heartbeatLoop periodically updates worker heartbeat and checks for leader changes.
func (fc *FileCoordinator) heartbeatLoop(ctx context.Context) {
	defer fc.wg.Done()

	ticker := time.NewTicker(fc.cfg.HeartbeatTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fc.stopCh:
			return
		case <-ticker.C:
			fc.updateHeartbeat()
			fc.checkLeader(ctx)
		}
	}
}

// updateHeartbeat updates this worker's heartbeat timestamp.
func (fc *FileCoordinator) updateHeartbeat() {
	fc.mu.Lock()
	fc.worker.LastHeartbeat = time.Now()
	fc.mu.Unlock()
	fc.persistWorkers()
}

// checkLeader performs leader election using a lock.
func (fc *FileCoordinator) checkLeader(ctx context.Context) {
	// Try to acquire leader lock
	acquired, err := fc.AcquireLock(ctx, "leader", fc.cfg.LockTTL)
	if err != nil {
		return
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	if acquired {
		if fc.leaderID != fc.worker.ID {
			fc.leaderID = fc.worker.ID
			if fc.leaderCB != nil {
				go fc.leaderCB(fc.worker.ID)
			}
		}
	} else {
		// Read current leader from lock file
		lockPath := filepath.Join(fc.cfg.LockDir, "leader.lock")
		data, err := os.ReadFile(lockPath)
		if err == nil {
			leaderID := string(data)
			if fc.leaderID != leaderID {
				fc.leaderID = leaderID
				if fc.leaderCB != nil {
					go fc.leaderCB(leaderID)
				}
			}
		}
	}
}

// renewLock periodically renews a lock by updating the timestamp.
func (fc *FileCoordinator) renewLock(ctx context.Context, key string, f *os.File, ttl time.Duration) {
	defer fc.wg.Done()

	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fc.stopCh:
			return
		case <-ticker.C:
			fc.mu.RLock()
			_, ok := fc.lockFiles[key]
			fc.mu.RUnlock()
			if !ok {
				return // Lock was released
			}
			// Just sync to keep the file alive
			f.Sync()
		}
	}
}

// persistWorkers writes the worker registry to a file.
func (fc *FileCoordinator) persistWorkers() error {
	workersPath := filepath.Join(fc.cfg.LockDir, "workers.json")
	fc.mu.RLock()
	data, err := json.MarshalIndent(fc.workers, "", "  ")
	fc.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(workersPath, data, 0644)
}