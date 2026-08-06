package robots

import (
	"context"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// RobotsEntry holds the parsed rules from a site's robots.txt file.
type RobotsEntry struct {
	Disallow   []string
	Allow      []string
	CrawlDelay time.Duration
	Sitemaps   []string
	fetchedAt  time.Time
	// transient marks an entry fetched during a transient server error (5xx).
	// Such entries are re-fetched after transientTTL instead of cacheTTL so a
	// momentary outage does not block the host for the full cache lifetime.
	transient bool
}

// RobotsParser fetches, caches, and evaluates robots.txt rules for hosts.
type RobotsParser struct {
	cache        map[string]*RobotsEntry
	mu           sync.RWMutex
	cacheTTL     time.Duration
	transientTTL time.Duration
	userAgent    string
	ruleCache    map[string]*regexp.Regexp
	ruleCacheMu  sync.RWMutex
	fetchGroup   singleflight.Group
	parentCtx    context.Context
}
