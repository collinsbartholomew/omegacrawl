package crawler

import (
	"net/http"

	"github.com/user/clone/internal/api"
	"github.com/user/clone/internal/util"
	"github.com/user/clone/internal/webui"
)

func (c *Crawler) Status() api.CrawlStatus {
	pages, assets, errors, bytes := c.metrics.Snapshot()
	c.seedsMu.RLock()
	seedCount := len(c.seeds)
	c.seedsMu.RUnlock()
	c.queueMu.RLock()
	queueSize := c.urlQueue.Size()
	c.queueMu.RUnlock()
	return api.CrawlStatus{
		PagesFetched: pages,
		AssetsSaved:  assets,
		Errors:       errors,
		BytesTotal:   bytes,
		QueueSize:    queueSize,
		Running:      c.started.Load(),
		Paused:       c.paused.Load(),
		SeedURLs:     seedCount,
	}
}

// IsRunning reports whether a crawl is currently in progress.
func (c *Crawler) IsRunning() bool {
	return c.started.Load()
}

// Pause temporarily stops dispatching new URLs from the queue. Already-running
// pages finish normally; no new URLs are popped until Resume is called.
func (c *Crawler) Pause() {
	if c.paused.CompareAndSwap(false, true) {
		util.LogInfo("crawl paused")
	}
}

// Resume re-enables URL dispatching after a Pause.
func (c *Crawler) Resume() {
	if c.paused.CompareAndSwap(true, false) {
		util.LogInfo("crawl resumed")
		select {
		case c.resumeCh <- struct{}{}:
		default:
		}
	}
}

// WasInterrupted reports whether the last crawl was stopped via Stop() (e.g.
// a user interrupt) rather than completing on its own.
func (c *Crawler) WasInterrupted() bool {
	return c.shutdown.Load()
}

func (c *Crawler) MetricsHandler() http.Handler {
	return c.metrics.PrometheusHandler()
}

func (c *Crawler) Stats() webui.CrawlStats {
	pages, assets, errors, bytes := c.metrics.Snapshot()
	c.queueMu.RLock()
	queueSize := c.urlQueue.Size()
	c.queueMu.RUnlock()
	return webui.CrawlStats{
		PagesFetched: pages,
		AssetsSaved:  assets,
		Errors:       errors,
		BytesTotal:   bytes,
		QueueSize:    queueSize,
		Running:      c.started.Load(),
		Paused:       c.paused.Load(),
	}
}
