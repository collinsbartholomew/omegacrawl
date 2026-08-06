package crawler

import (
	"time"
)

func (c *Crawler) periodicBrowserHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if c.browserPool != nil {
				c.browserPool.HealthCheck()
			}
		case <-c.ctx.Done():
			return
		}
	}
}
