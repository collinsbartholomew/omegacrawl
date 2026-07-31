package crawler

import (
	"context"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/jsengine"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/resilience"
)

func cfgUserAgent(cfg *config.Config) string {
	if cfg.RotateUserAgents && len(cfg.UserAgents) > 0 {
		return cfg.UserAgents[rand.Intn(len(cfg.UserAgents))]
	}
	return cfg.UserAgent
}

func getHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func isValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (c *Crawler) normalizeURL(rawURL string) string {
	if c.cfg.NormalizeURLs {
		return queue.NormalizeURL(rawURL)
	}
	return rawURL
}

func (c *Crawler) saveCheckpoint() {
	c.checkpointMu.Lock()
	defer c.checkpointMu.Unlock()
	c.hostMu.RLock()
	hlc := make(map[string]time.Time, len(c.hostLastCrawl))
	for k, v := range c.hostLastCrawl {
		hlc[k] = v
	}
	huc := make(map[string]int, len(c.hostURLCount))
	for k, v := range c.hostURLCount {
		huc[k] = v
	}
	c.hostMu.RUnlock()
	
	// Get atomic snapshot of queue
	items, visited := c.urlQueue.Snapshot()
	c.checkpoint.Save(items, visited, hlc, huc)
}

func (c *Crawler) getHostSem(host string) *hostSem {
	c.hostMu.Lock()
	defer c.hostMu.Unlock()
	sem, ok := c.hostSemaphores[host]
	if !ok {
		sem = &hostSem{ch: make(chan struct{}, 2)}
		c.hostSemaphores[host] = sem
	}
	return sem
}

func (c *Crawler) selectProxy() string {
	if c.cfg.Proxy != "" {
		return c.cfg.Proxy
	}
	if len(c.cfg.Proxies) > 0 {
		return c.cfg.Proxies[rand.Intn(len(c.cfg.Proxies))]
	}
	return ""
}

func (c *Crawler) isAllowedDomain(rawURL string) bool {
	if len(c.cfg.AllowedDomains) == 0 {
		return true
	}
	host := getHost(rawURL)
	for _, domain := range c.cfg.AllowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (c *Crawler) isExcluded(rawURL string) bool {
	return c.excludeFn(rawURL)
}

func (c *Crawler) isSameDomain(original, target string) bool {
	if original == "" {
		return true
	}
	return getHost(original) == getHost(target)
}

func (c *Crawler) applyWaitStrategy(ctx context.Context) {
	waitCtx, waitCancel := context.WithTimeout(ctx, c.cfg.WaitStrategyTimeout)
	defer waitCancel()

	switch c.cfg.WaitStrategy {
	case "selector":
		if c.cfg.WaitSelector != "" {
			jsengine.WaitForSelector(waitCtx, c.cfg.WaitSelector, c.cfg.WaitTimeout)
		}
	case "networkidle":
		jsengine.WaitForNetworkIdle(waitCtx, c.cfg.NetworkIdleQuiet)
	case "response":
		if c.cfg.WaitForResponse != "" {
			strategy := &jsengine.WaitForResponseStrategy{
				URLPattern: c.cfg.WaitForResponse,
				Timeout:    c.cfg.WaitTimeout,
			}
			strategy.Wait(waitCtx)
		}
	case "adaptive":
		detection, err := jsengine.DetectFramework(waitCtx)
		if err == nil && detection != nil {
			strategy := &jsengine.AdaptiveWaitStrategy{
				Framework:    detection.Framework,
				SelectorWait: c.cfg.WaitTimeout,
				NetworkWait:  c.cfg.NetworkIdleQuiet,
				MaxWait:      c.cfg.WaitTimeout,
			}
			strategy.Wait(waitCtx)
		} else {
			jsengine.WaitForNetworkIdle(waitCtx, c.cfg.NetworkIdleQuiet)
		}
	default:
		waitTimer := time.NewTimer(c.cfg.WaitStrategyTimeout)
		select {
		case <-waitTimer.C:
		case <-ctx.Done():
			if !waitTimer.Stop() {
				<-waitTimer.C
			}
		}
	}
}

// shouldQueue checks if a URL should be added to the crawl queue.
// If it passes all filters, it's marked as seen and true is returned.

func (c *Crawler) shouldQueue(normalized string) bool {
	if !isValidURL(normalized) {
		return false
	}
	if c.bloomFilter.HasSeen(normalized) {
		return false
	}
	if !c.isAllowedDomain(normalized) {
		return false
	}
	if c.isExcluded(normalized) {
		return false
	}
	if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
		return false
	}
	if c.urlQueue.Size() >= maxQueueSize {
		return false
	}
	if c.exactDedup.Contains(normalized) {
		return false
	}
	c.bloomFilter.Add(normalized)
	c.exactDedup.Add(normalized)
	return true
}

func (c *Crawler) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.pruneUnboundedMaps()
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
			c.metrics.SetQueueSize(int64(c.urlQueue.Size()))
			c.hostMu.RLock()
			c.metrics.SetActiveHosts(int64(len(c.hostURLCount)))
			c.hostMu.RUnlock()
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
