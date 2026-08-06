package config

import (
	"time"
)

// DefaultConfig returns a new Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Seeds:                []string{},
		MaxDepth:             10,
		MaxConcurrentPages:   5,
		AssetConcurrency:     16,
		PageTimeout:          120 * time.Second,
		UserAgent:            "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		OutputDir:            "output",
		CrawlDelay:           1 * time.Second,
		RespectRobots:        true,
		EnableScreenshot:     true,
		EnablePDF:            false,
		EnableWARC:           false,
		EnableWACZ:           false,
		EnableSingleFile:     true,
		EnableArticleExtract: true,
		ScrollHeight:         5000,
		MaxRetries:           3,
		MaxURLsPerHost:       10000,
		MaxTotalURLs:         100000,
		CheckpointInterval:   5 * time.Minute,
		CheckpointFile:       "",
		BloomFilterPath:      "",
		RotateUserAgents:     false,
		UserAgents: []string{
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		},
		WaitStrategy:        "adaptive",
		WaitSelector:        "",
		WaitTimeout:         60 * time.Second,
		NetworkIdleQuiet:    1 * time.Second,
		WaitForPageTimeout:  30 * time.Second,
		WaitStrategyTimeout: 60 * time.Second,

		InfiniteScroll: &InfiniteScrollConfig{
			Enabled:          true,
			MaxScrolls:       20,
			MaxDuration:      10 * time.Second,
			StablePasses:     3,
			ItemSelector:     "article, .card, .list-item, [data-infinite-scroll-item], .feed-item",
			ScrollContainer:  "",
			LoadMoreSelector: "",
			ScrollDelay:      2 * time.Second,
			ScrollDistance:   500,
		},

		ClickSelectors:  []string{},
		JSBeforeLoad:    []string{},
		JSAfterLoad:     []string{},
		ExpandSections:  true,
		DismissOverlays: true,

		EnableStealth:        true,
		EnableLazyLoad:       true,
		EnableShadowDOM:      true,
		EnableIframes:        true,
		EnableRouteDiscovery: true,
		EnableMediaCapture:   true,
		EnableStructuredData: true,
		MaxSPARoutes:         50,

		EnableInteractionEngine: false,
		MaxInteractionsPerPage:  50,

		ViewportWidth:  1920,
		ViewportHeight: 1080,

		MobileEmulation: false,
		MobileDevice:    "",
		MobileUserAgent: "",

		NormalizeURLs: true,

		MaxIframeDepth:     2,
		IframeSkipPatterns: []string{"googleads", "doubleclick", "facebook.com/tr"},

		EnableAPICapture: true,
		DisableTLSVerify: false,
		BrowserPoolSize:  1,
		Incremental:      false,
		IncCacheFile:     "",
		MinDiskSpace:     1024 * 1024 * 1024, // 1GB default

		AuthConfig:            &AuthConfig{},
		CAPTCHAConfig:         &CAPTCHAConfig{},
		QueueConfig:           &QueueConfig{Backend: "local"},
		ChangeDetectionConfig: &ChangeDetectionConfig{Enabled: false, MaxSnapshots: 10},
		Dedupe:                &DedupeConfig{},
	}
}
