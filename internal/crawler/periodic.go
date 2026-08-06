package crawler

import (
	"syscall"
	"time"

	"github.com/user/clone/internal/resilience"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.pruneUnboundedMaps()
			c.circuitBreaker.Cleanup()
		}
	}
}

func (c *Crawler) pruneUnboundedMaps() {
	c.hostMu.Lock()
	cutoff := time.Now().Add(-24 * time.Hour)
	for host, last := range c.hostLastCrawl {
		if last.Before(cutoff) {
			delete(c.hostLastCrawl, host)
			delete(c.hostURLCount, host)
			if sem, ok := c.hostSemaphores[host]; ok {
				sem.closed.Store(true)
				delete(c.hostSemaphores, host)
			}
		}
	}
	c.hostMu.Unlock()

	c.routeMu.Lock()
	if len(c.discoveredRoutes) > 50000 {
		c.discoveredRoutes = make(map[string]bool)
	}
	c.routeMu.Unlock()
}

func (c *Crawler) periodicCheckpoint() {
	ticker := time.NewTicker(c.cfg.CheckpointInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.checkpointDone:
			return
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.cfg.CheckpointFile != "" {
				c.saveCheckpoint()
			}
		}
	}
}

func (c *Crawler) periodicMetricsUpdate() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.queueMu.RLock()
			c.metrics.SetQueueSize(int64(c.urlQueue.Size()))
			c.queueMu.RUnlock()
			c.hostMu.RLock()
			c.metrics.SetActiveHosts(int64(len(c.hostURLCount)))
			c.hostMu.RUnlock()
			// Reset gauges each tick, then recount: they reflect the current
			// count of open/half-open breakers, not an ever-growing total.
			c.metrics.ResetCircuitOpen()
			c.metrics.ResetCircuitHalfOpen()
			c.circuitBreaker.RangeStates(func(state resilience.State) {
				switch state {
				case resilience.StateOpen:
					c.metrics.IncCircuitOpen()
				case resilience.StateHalfOpen:
					c.metrics.IncCircuitHalfOpen()
				}
			})
		}
	}
}

func (c *Crawler) periodicDiskSpaceCheck() {
	if c.cfg.MinDiskSpace <= 0 {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			free, err := diskFreeSpace(c.cfg.OutputDir)
			if err != nil {
				util.LogError("failed to check disk space", err)
				continue
			}
			if free < c.cfg.MinDiskSpace {
				util.LogError("insufficient disk space", nil,
					zap.Int64("free_bytes", free),
					zap.Int64("min_required_bytes", c.cfg.MinDiskSpace),
				)
				c.Stop()
				return
			}
		}
	}
}

func diskFreeSpace(path string) (int64, error) {
	// Use syscall.Statfs to get disk free space
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
