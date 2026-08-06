package crawler

import "time"

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

	c.queueMu.RLock()
	items, visited := c.urlQueue.Snapshot()
	c.queueMu.RUnlock()
	c.checkpoint.Save(items, visited, hlc, huc)
}

func (c *Crawler) shouldQueue(normalized string) bool {
	c.queueMu.RLock()
	defer c.queueMu.RUnlock()
	
	if !isValidURL(normalized) {
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
	// Consult the authoritative exact-dedup set first. The bloom filter is only
	// a secondary seen-memory used after an exact miss; consulting it first would
	// let a bloom false positive permanently drop a genuinely-new URL.
	if c.exactDedup.Contains(normalized) {
		return false
	}
	if c.bloomFilter.HasSeen(normalized) {
		return false
	}
	c.bloomFilter.Add(normalized)
	c.exactDedup.Add(normalized)
	return true
}
