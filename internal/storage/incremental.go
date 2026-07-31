package storage

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// ResourceCacheEntry stores HTTP validation metadata for a cached resource.
type ResourceCacheEntry struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	StatusCode   int       `json:"status_code,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
}

// ResourceCache tracks conditional request metadata (ETag, Last-Modified)
// per URL to enable incremental re-crawling. Changes can be persisted to a
// JSON file on disk.
type ResourceCache struct {
	mu      sync.RWMutex
	entries map[string]*ResourceCacheEntry
	path    string
	dirty   bool
}

// NewResourceCache creates a cache backed by the JSON file at path, loading
// any existing entries. If path is empty the cache is memory-only.
func NewResourceCache(path string) *ResourceCache {
	c := &ResourceCache{
		entries: make(map[string]*ResourceCacheEntry),
		path:    path,
	}
	if path != "" {
		c.load()
	}
	return c
}

func (c *ResourceCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var entries map[string]*ResourceCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
}

// Save persists the cache to disk if it has been modified since the last
// save.
func (c *ResourceCache) Save() error {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	entries := make(map[string]*ResourceCacheEntry, len(c.entries))
	for k, v := range c.entries {
		entries[k] = v
	}
	c.dirty = false
	c.mu.Unlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	if err := os.WriteFile(c.path, data, 0600); err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	return nil
}

// Get returns the cache entry for url, if present.
func (c *ResourceCache) Get(url string) (*ResourceCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	if !ok {
		return nil, false
	}
	return e, true
}

// Set stores entry for url and marks the cache dirty.
func (c *ResourceCache) Set(url string, entry *ResourceCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = entry
	c.dirty = true
}

// ConditionalHeaders returns the stored ETag and Last-Modified values for
// url, which may be sent as conditional request headers.
func (c *ResourceCache) ConditionalHeaders(url string) (etag, lastModified string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	if !ok {
		return "", ""
	}
	return e.ETag, e.LastModified
}

// UpdateFromResponse records validation headers from an HTTP response for
// url, updating the entry only when new validation data is available.
func (c *ResourceCache) UpdateFromResponse(url string, statusCode int, headers map[string]string) {
	etag := headers["Etag"]
	if etag == "" {
		etag = headers["etag"]
	}
	lm := headers["Last-Modified"]
	if lm == "" {
		lm = headers["last-modified"]
	}
	if etag == "" && lm == "" && statusCode != 304 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	existing, ok := c.entries[url]
	if !ok {
		existing = &ResourceCacheEntry{}
		c.entries[url] = existing
	}
	if etag != "" {
		existing.ETag = etag
	}
	if lm != "" {
		existing.LastModified = lm
	}
	if statusCode > 0 {
		existing.StatusCode = statusCode
	}
	existing.FetchedAt = time.Now()
	c.dirty = true
}
