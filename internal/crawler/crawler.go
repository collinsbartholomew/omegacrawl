package crawler

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"strings"
	"sync"
	"time"

	cdpnetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/user/clone/internal/auth"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/changedetection"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/coordinator"
	"github.com/user/clone/internal/network"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/robots"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"

	clientpool "github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/ratelimit"
	"github.com/user/clone/internal/resilience"
)

// NewCrawler creates a new Crawler instance with the given configuration.
// It initializes all internal components including the browser pool, URL queue,
// rate limiter, circuit breaker, and optional coordinator for distributed crawling.
// The returned crawler must be started with Start() and stopped with Stop().
func NewCrawler(cfg *config.Config) (*Crawler, error) {
	// Validate configuration at startup
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	store := storage.NewFilesystem(cfg)
	robotsParser := robots.NewRobotsParser()
	robotsParser.SetUserAgent(cfg.UserAgent)
	rw := rewrite.NewRewriter()
	optRw := rewrite.NewOptimizedRewriter(rw)

	ctx, cancel := context.WithCancel(context.Background())
	robotsParser.SetContext(ctx)

	var bloomFilter *queue.BloomDedup
	expectedItems := uint(cfg.MaxTotalURLs * 10)
	if expectedItems < 1000 {
		expectedItems = 1000
	}
	bloomFilter = queue.NewBloomDedup(expectedItems, 0.01)
	if cfg.BloomFilterPath != "" {
		if _, err := os.Stat(cfg.BloomFilterPath); err == nil {
			bloomFilter.LoadFromFile(cfg.BloomFilterPath)
		}
	}

	retryConfig := DefaultRetryConfig()
	if cfg.MaxRetries > 0 {
		retryConfig.MaxRetries = cfg.MaxRetries
	}

	maxConcurrent := cfg.MaxConcurrentPages
	if maxConcurrent < 1 || cfg.Interactive {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)

	assetConcurrency := cfg.AssetConcurrency
	if assetConcurrency < 4 {
		assetConcurrency = 16
	}
	assetSem := make(chan struct{}, assetConcurrency)

	urlQueue, err := queue.NewQueueFromConfig(ctx, cfg.QueueConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}
	clientPool := clientpool.NewClientPool(&clientpool.ClientConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		ConnectTimeout:        15 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DisableCompression:    false,
	})
	httpClient := clientPool.Client()

	lruSize := cfg.MaxTotalURLs
	if lruSize < 100000 {
		lruSize = 100000
	}

	hostLastCrawl := make(map[string]time.Time)
	hostURLCount := make(map[string]int)
	hostSemaphores := make(map[string]*hostSem)
	discoveredRoutes := make(map[string]bool)

	c := &Crawler{
		cfg:                cfg,
		storage:            store,
		robotsParser:       robotsParser,
		rewriter:           rw,
		optimizedRewriter:  optRw,
		urlQueue:           urlQueue,
		bloomFilter:      bloomFilter,
		exactDedup:       util.NewLRUSet(lruSize),
		rateLimiter:      ratelimit.New(ctx, cfg.CrawlDelay, 1),
		circuitBreaker:   resilience.NewHostCircuitBreaker(),
		retryConfig:      retryConfig,
		checkpoint:       NewCheckpoint(cfg.CheckpointFile),
		semaphore:        sem,
		assetSem:         assetSem,
		httpClient:       httpClient,
		ctx:              ctx,
		cancel:           cancel,
		hostLastCrawl:    hostLastCrawl,
		hostURLCount:     hostURLCount,
		hostSemaphores:   hostSemaphores,
		contentHashes:    util.NewLRUCache(maxContentHashes),
		contentHashBloom:  queue.NewBloomDedup(uint(maxContentHashes*10), 0.001),
		discoveredRoutes: discoveredRoutes,
		jsErrors:         util.NewBoundedQueue(maxJSErrors),
		wsMessages:       util.NewBoundedQueue(maxWSMessages),
		apiResponses:     util.NewBoundedQueue(maxAPICaptures),
		checkpointDone:   make(chan struct{}),
		metrics:          &util.Metrics{},
		incCache:         storage.NewResourceCache(cfg.IncCacheFile),
		cookieJar:        make(map[string][]*http.Cookie),
		wsURLs:           make(map[cdpnetwork.RequestID]string),
		hostMu:           sync.RWMutex{},
		routeMu:          sync.RWMutex{},
		seeds:            append([]string{}, cfg.Seeds...),
		interceptorPool:  network.NewInterceptorPool(cfg.MaxConcurrentPages, cfg.MaxConcurrentPages*2),
		resumeCh:         make(chan struct{}, 1),
	}

	if len(cfg.ExcludePatterns) > 0 {
		patterns := make([]string, len(cfg.ExcludePatterns))
		copy(patterns, cfg.ExcludePatterns)
		c.excludeFn = func(rawURL string) bool {
			for _, p := range patterns {
				if strings.Contains(rawURL, p) {
					return true
				}
			}
			return false
		}
	} else {
		c.excludeFn = func(string) bool { return false }
	}

	c.authManager = auth.NewAuthManager(c.cfg.AuthConfig)

	if cfg.EnableWARC {
		c.warc = storage.NewWARCWriter(cfg.OutputDir)
	}
	if cfg.EnableWACZ {
		c.wacz = storage.NewWACZWriter(cfg.OutputDir)
	}

	if cfg.ChangeDetectionConfig != nil && cfg.ChangeDetectionConfig.Enabled {
		snapDir := cfg.ChangeDetectionConfig.SnapshotDir
		if snapDir == "" {
			snapDir = cfg.OutputDir + "/snapshots"
		}
		reportDir := cfg.ChangeDetectionConfig.ReportDir
		if reportDir == "" {
			reportDir = cfg.OutputDir + "/change_reports"
		}
		c.changeDetector = changedetection.NewDetector(changedetection.DetectorConfig{
			SnapshotDir:  snapDir,
			MaxSnapshots: cfg.ChangeDetectionConfig.MaxSnapshots,
			ReportDir:    reportDir,
			EnableDiff:   true,
		})
	}

	if !cfg.Interactive && cfg.CAPTCHAConfig != nil && cfg.CAPTCHAConfig.Enabled {
		c.captchaSolver = captcha.NewSolver(cfg.CAPTCHAConfig)
	}

	c.memoryBudget = util.NewMemoryBudget(0)

	// Initialize coordinator for distributed worker coordination
	coordCfg := coordinator.DefaultConfig()
	if cfg.QueueConfig != nil && cfg.QueueConfig.Backend == "redis" {
		coordCfg.Backend = coordinator.BackendRedis
		coordCfg.RedisURL = cfg.QueueConfig.RedisURL
	} else if cfg.QueueConfig != nil && cfg.QueueConfig.Backend == "postgres" {
		// For now, use file-based coordinator for non-redis backends
		coordCfg.Backend = coordinator.BackendFile
	} else if cfg.QueueConfig != nil && cfg.QueueConfig.Backend == "kafka" {
		coordCfg.Backend = coordinator.BackendFile
	} else {
		coordCfg.Backend = coordinator.BackendNone
	}
	coord, err := coordinator.NewCoordinator(coordCfg)
	if err != nil {
		util.LogError("failed to create coordinator", err)
	} else {
		c.coordinator = coord
	}

	return c, nil
}

func (c *Crawler) Stop() {
	c.shutdown.Store(true)
	c.cancel()
	c.rateLimiter.Stop()
	if c.coordinator != nil {
		if err := c.coordinator.Stop(); err != nil {
			util.LogError("failed to stop coordinator", err)
		}
	}
	if c.browserPool != nil {
		c.browserPool.Close()
	}
	if c.urlQueue != nil {
		if err := c.urlQueue.Close(); err != nil {
			util.LogError("failed to close URL queue", err)
		}
	}
	if c.interceptorPool != nil {
		c.interceptorPool.Close()
	}
}

// extractTextContent extracts readable text content from the page
func (c *Crawler) extractTextContent(ctx context.Context) string {
	var textContent string
	chromedp.Run(ctx, chromedp.Evaluate(`
		(function() {
			var text = document.body ? document.body.innerText : '';
			return text.replace(/\s+/g, ' ').trim();
		})()
	`, &textContent))
	return textContent
}

// extractPageStructure extracts structural elements from the page for change detection
func (c *Crawler) extractPageStructure(ctx context.Context) []changedetection.StructureElement {
	var elements []changedetection.StructureElement
	
	// Get all elements with their selectors and text hashes
	chromedp.Run(ctx, chromedp.Evaluate(`
		(function() {
			var elements = [];
			var walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
			var node;
			while (node = walker.nextNode()) {
				var selector = getSelector(node);
				var text = node.innerText || '';
				var hash = simpleHash(text);
				elements.push({
					tag: node.tagName.toLowerCase(),
					selector: selector,
					textHash: hash,
					childCount: node.children.length,
					attrs: getAttributes(node)
				});
			}
			return elements;
			
			function getSelector(el) {
				if (el.id) return '#' + el.id;
				if (el.className) return '.' + el.className.split(' ')[0];
				var path = [];
				while (el && el !== document.body) {
					var idx = 1;
					var sibling = el.previousElementSibling;
					while (sibling) {
						idx++;
						sibling = sibling.previousElementSibling;
					}
					path.unshift(el.tagName.toLowerCase() + ':nth-child(' + idx + ')');
					el = el.parentElement;
				}
				return path.join(' > ');
			}
			
			function getAttributes(el) {
				var attrs = {};
				for (var i = 0; i < el.attributes.length; i++) {
					var attr = el.attributes[i];
					attrs[attr.name] = attr.value;
				}
				return attrs;
			}
			
			function simpleHash(str) {
				var hash = 0;
				for (var i = 0; i < str.length; i++) {
					hash = ((hash << 5) - hash) + str.charCodeAt(i);
					hash |= 0;
				}
				return hash.toString(16);
			}
		})()
	`, &elements))
	
	return elements
}
