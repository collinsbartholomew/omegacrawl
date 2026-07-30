package config

import "time"

const (
	MaxResponseBodySize = 50 * 1024 * 1024
	MaxRetries          = 3
	MaxSeenURLs         = 200000
	MaxResources        = 50000
	MaxAPIResponses     = 10000
	MaxRedirectChains   = 10000

	MaxContentHashes   = 200000
	MaxJSErrors        = 10000
	MaxWSMessages      = 5000
	MaxAPICaptures     = 2000
	MaxQueueSize       = 100000
	MaxCookiesPerDomain = 50

	DrainTimeout = 30 * time.Second
)
