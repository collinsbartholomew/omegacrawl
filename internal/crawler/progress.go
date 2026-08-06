package crawler

import (
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) reportProgress() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.checkpointDone:
			return
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			pages, assets, errors, bytes := c.metrics.Snapshot()
			util.LogInfo("progress",
				zap.Int64("pages", pages),
				zap.Int64("assets", assets),
				zap.Int64("errors", errors),
				zap.Int64("bytes", bytes),
				zap.Int("queue", c.urlQueue.Size()),
			)
		}
	}
}
