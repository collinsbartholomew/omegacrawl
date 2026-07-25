package storage

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type ResourceCacheEntry struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	StatusCode   int       `json:"status_code,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
}

type ResourceCache struct {
	mu        sync.RWMutex
	entries   map[string]*ResourceCacheEntry
	path      string
	dirty     bool
}

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
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	return nil
}

func (c *ResourceCache) Get(url string) (*ResourceCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	if !ok {
		return nil, false
	}
	return e, true
}

func (c *ResourceCache) Set(url string, entry *ResourceCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = entry
	c.dirty = true
}

func (c *ResourceCache) ConditionalHeaders(url string) (etag, lastModified string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[url]
	if !ok {
		return "", ""
	}
	return e.ETag, e.LastModified
}

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
