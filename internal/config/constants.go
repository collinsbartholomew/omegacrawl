package config

import "time"

const (
	// MaxResponseBodySize is the maximum HTTP response body size the crawler will retain.
	MaxResponseBodySize = 50 * 1024 * 1024
	// MaxRetries is the default number of retry attempts for failed requests.
	MaxRetries = 3
	// MaxSeenURLs is the maximum number of unique URLs tracked for deduplication.
	MaxSeenURLs = 200000
	// MaxResources is the maximum number of page resources retained.
	MaxResources = 50000
	// MaxAPIResponses is the maximum number of captured API responses.
	MaxAPIResponses = 10000
	// MaxRedirectChains is the maximum length of a redirect chain.
	MaxRedirectChains = 10000

	// MaxContentHashes is the maximum number of content hashes stored for deduplication.
	MaxContentHashes = 200000
	// MaxJSErrors is the maximum number of captured JavaScript errors.
	MaxJSErrors = 10000
	// MaxWSMessages is the maximum number of captured WebSocket messages.
	MaxWSMessages = 5000
	// MaxAPICaptures is the maximum number of captured API interactions.
	MaxAPICaptures = 2000
	// MaxQueueSize is the maximum number of URLs held in the queue.
	MaxQueueSize = 100000
	// MaxCookiesPerDomain is the maximum number of cookies stored per domain.
	MaxCookiesPerDomain = 50

	// DrainTimeout is the maximum time to wait when draining outstanding work during shutdown.
	DrainTimeout = 30 * time.Second
)
