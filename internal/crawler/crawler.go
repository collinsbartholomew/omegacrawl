package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/api"
	"github.com/user/clone/internal/auth"
	"github.com/user/clone/internal/browserpool"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/changedetection"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/pool"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/robots"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"github.com/user/clone/internal/webui"

	crawlerrors "github.com/user/clone/internal/errors"
	clientpool "github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/ratelimit"
	"github.com/user/clone/internal/resilience"
)

type JSError struct {
	URL     string `json:"url"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

type WSMsg struct {
	URL       string    `json:"url"`
	Direction string    `json:"direction"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Opcode    float64   `json:"opcode"`
	IsBinary  bool      `json:"is_binary,omitempty"`
}

type CapturedAPIResponse struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	StatusCode  int               `json:"status_code"`
	Body        []byte            `json:"body"`
	Headers     map[string]string `json:"headers"`
	RequestBody []byte            `json:"request_body,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Size        int               `json:"size"`
	GraphQLOp   string            `json:"graphql_op,omitempty"`
}

const maxContentHashes = 1000000
const maxJSErrors = 10000
const maxWSMessages = 5000
const maxWSFrameSize = 10 * 1024 * 1024 // 10MB max per WS frame
const maxAPICaptures = 2000
const maxQueueSize = 100000
const drainTimeout = 30 * time.Second
const maxCookiesPerDomain = 50

func NewCrawler(cfg *config.Config) (*Crawler, error) {
	store := storage.NewFilesystem(cfg)
	robotsParser := robots.NewRobotsParser()
	robotsParser.SetUserAgent(cfg.UserAgent)
	rw := rewrite.NewRewriter()

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

	urlQueue, err := queue.NewQueueFromConfig(ctx, cfg.QueueConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}
	var persistentQueue *queue.PersistentQueue
	if pq, ok := urlQueue.(*queue.PersistentQueue); ok {
		persistentQueue = pq
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
		cfg:              cfg,
		storage:          store,
		robotsParser:     robotsParser,
		rewriter:         rw,
		urlQueue:         urlQueue,
		persistentQueue:  persistentQueue,
		bloomFilter:      bloomFilter,
		exactDedup:       util.NewLRUSet(lruSize),
		rateLimiter:      ratelimit.New(ctx, cfg.CrawlDelay, 1),
		circuitBreaker:   resilience.NewHostCircuitBreaker(),
		retryConfig:      retryConfig,
		checkpoint:       NewCheckpoint(cfg.CheckpointFile),
		semaphore:        sem,
		httpClient:       httpClient,
		ctx:              ctx,
		cancel:           cancel,
		hostLastCrawl:    hostLastCrawl,
		hostURLCount:     hostURLCount,
		hostSemaphores:   hostSemaphores,
		contentHashes:    util.NewLRUSet(maxContentHashes),
		discoveredRoutes: discoveredRoutes,
		jsErrors:         util.NewBoundedQueue(maxJSErrors),
		wsMessages:       util.NewBoundedQueue(maxWSMessages),
		apiResponses:     util.NewBoundedQueue(maxAPICaptures),
		checkpointDone:   make(chan struct{}),
		metrics:          &util.Metrics{},
		incCache:         storage.NewResourceCache(cfg.IncCacheFile),
		cookieJar:        make(map[string][]*http.Cookie),
		wsURLs:           make(map[network.RequestID]string),
		hostMu:           sync.RWMutex{},
		routeMu:          sync.RWMutex{},
		browserMu:        sync.Mutex{},
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
		c.changeDetector = changedetection.NewDetector(snapDir)
	}

	if !cfg.Interactive && cfg.CAPTCHAConfig != nil && cfg.CAPTCHAConfig.Enabled {
		c.captchaSolver = captcha.NewSolver(cfg.CAPTCHAConfig)
	}

	c.memoryBudget = util.NewMemoryBudget(0)

	return c, nil
}

func (c *Crawler) Stop() {
	c.shutdown.Store(true)
	c.cancel()
	c.rateLimiter.Stop()
	if c.browserPool != nil {
		c.browserPool.Close()
	}
}

func (c *Crawler) Status() api.CrawlStatus {
	pages, assets, errors, bytes := c.metrics.Snapshot()
	return api.CrawlStatus{
		PagesFetched: pages,
		AssetsSaved:  assets,
		Errors:       errors,
		BytesTotal:   bytes,
		QueueSize:    c.urlQueue.Size(),
		Running:      !c.shutdown.Load(),
		SeedURLs:     len(c.cfg.Seeds),
	}
}

// MetricsHandler returns an HTTP handler serving crawl metrics in Prometheus
// exposition format, satisfying api.MetricsHandler.

func (c *Crawler) MetricsHandler() http.Handler {
	return c.metrics.PrometheusHandler()
}

func (c *Crawler) Stats() webui.CrawlStats {
	pages, assets, errors, bytes := c.metrics.Snapshot()
	return webui.CrawlStats{
		PagesFetched: pages,
		AssetsSaved:  assets,
		Errors:       errors,
		BytesTotal:   bytes,
		QueueSize:    c.urlQueue.Size(),
		Running:      !c.shutdown.Load(),
	}
}

func (c *Crawler) Start(seeds []string) error {
	defer c.cancel()
	defer c.rateLimiter.Stop()

	if c.cfg.CheckpointFile != "" && c.checkpoint.Exists() {
		data, err := c.checkpoint.Load()
		if err != nil {
			util.LogError("failed to load checkpoint", err)
		} else {
			c.urlQueue.LoadFromCheckpoint(data.Queue, data.Visited)
			c.hostLastCrawl = data.HostLastCrawl
			c.hostURLCount = data.HostURLCount
			util.LogInfo("resumed from checkpoint",
				zap.Int("queue_size", len(data.Queue)),
				zap.Int("visited_count", len(data.Visited)),
			)
		}
	}

	for _, seed := range seeds {
		normalized := c.normalizeURL(queue.NormalizeAndClean(seed))
		if !isValidURL(normalized) {
			continue
		}
		if c.bloomFilter.HasSeen(normalized) {
			continue
		}
		c.bloomFilter.Add(normalized)
		c.exactDedup.Add(normalized)
		c.urlQueue.PushURL(normalized, 0)

		if c.cfg.RespectRobots {
			sitemapURLs := c.robotsParser.GetSitemapURLs(normalized)
			for _, sitemapURL := range sitemapURLs {
				sNorm := c.normalizeURL(queue.NormalizeAndClean(sitemapURL))
				if isValidURL(sNorm) && !c.bloomFilter.HasSeen(sNorm) {
					c.bloomFilter.Add(sNorm)
					c.exactDedup.Add(sNorm)
					c.urlQueue.PushURL(sNorm, 0)
					util.LogDebug("seeded from sitemap", zap.String("url", sNorm))
				}
			}
		}
	}

	c.allocOpts = append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", !c.cfg.Interactive),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("window-size", fmt.Sprintf("%d,%d", c.cfg.ViewportWidth, c.cfg.ViewportHeight)),
		chromedp.Flag("disable-features", "TranslateUI,ChromeWhatsNewUI"),
		chromedp.Flag("disable-component-update", true),
	)
	if c.cfg.EnableStealth {
		c.allocOpts = append(c.allocOpts,
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
			chromedp.Flag("excludeSwitches", "enable-automation"),
			chromedp.Flag("disable-renderer-backgrounding", true),
		)
	}
	proxy := c.selectProxy()
	if proxy != "" {
		c.allocOpts = append(c.allocOpts, chromedp.ProxyServer(proxy))
	}
	if c.cfg.Interactive {
		c.allocOpts = append(c.allocOpts, chromedp.Flag("start-maximized", true))
	}
	if c.cfg.UserDataDir != "" {
		c.allocOpts = append(c.allocOpts, chromedp.Flag("user-data-dir", c.cfg.UserDataDir))
	}
	// Apply user-configured Chrome flags (override internal ones if conflicting)
	for _, flag := range c.cfg.ChromeFlags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		if strings.HasPrefix(flag, "--") {
			flag = strings.TrimPrefix(flag, "--")
		}
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) == 2 {
			c.allocOpts = append(c.allocOpts, chromedp.Flag(parts[0], parts[1]))
		} else {
			c.allocOpts = append(c.allocOpts, chromedp.Flag(flag, true))
		}
	}

	// Start browser pool
	c.browserPool = browserpool.New(c.ctx, c.cfg.BrowserPoolSize, c.allocOpts, c.cfg.RemoteChromeURL)
	if err := c.browserPool.Start(); err != nil {
		return fmt.Errorf("browser pool start failed: %w", err)
	}

	go c.periodicCheckpoint()
	go c.reportProgress()
	go c.periodicCleanup()
	go c.periodicCookieSave()
	go c.periodicBrowserHealthCheck()
	go c.periodicMetricsUpdate()

	c.loadCookieJar()

	if c.cfg.Interactive && c.cfg.AuthConfig != nil && c.cfg.AuthConfig.Enabled && c.cfg.AuthConfig.LoginURL != "" {
		fmt.Println("\n=== Pre-crawl Login Phase ===")
		fmt.Printf("Login URL: %s\n", c.cfg.AuthConfig.LoginURL)
		fmt.Println("Log in manually in the browser, then press Enter to begin crawling.")
		fmt.Print("Press Enter when ready (or 'q' to quit): ")

		loginTabCtx, loginTabRelease, err := c.browserPool.Acquire()
		if err != nil {
			c.closeWriters()
			return fmt.Errorf("failed to acquire browser for login: %w", err)
		}
		loginCtx, loginTimeoutCancel := context.WithTimeout(loginTabCtx, 30*time.Second)
		defer loginTimeoutCancel()
		if err := chromedp.Run(loginCtx, chromedp.Navigate(c.cfg.AuthConfig.LoginURL)); err != nil {
			loginTabRelease()
			c.closeWriters()
			return fmt.Errorf("failed to navigate to login URL: %w", err)
		}
		c.waitForPage(loginTabCtx)

		var input string
		if _, err := fmt.Scanln(&input); err == nil {
			if input == "q" || input == "Q" || input == "quit" || input == "exit" {
				loginTabRelease()
				c.closeWriters()
				util.LogInfo("user cancelled during pre-crawl login")
				return nil
			}
		}
		c.persistCookies(loginTabCtx, c.cfg.AuthConfig.LoginURL)
		loginTabRelease()
		fmt.Println("Login captured. Starting crawl...")
	}

	util.LogInfo("starting crawl",
		zap.Int("seeds", len(seeds)),
		zap.Int("max_concurrent", c.cfg.MaxConcurrentPages),
		zap.Int("max_depth", c.cfg.MaxDepth),
		zap.Int("max_urls_per_host", c.cfg.MaxURLsPerHost),
		zap.String("wait_strategy", c.cfg.WaitStrategy),
		zap.Bool("infinite_scroll", c.cfg.InfiniteScroll.Enabled),
		zap.Bool("stealth", c.cfg.EnableStealth),
		zap.Bool("respect_robots", c.cfg.RespectRobots),
		zap.Bool("interactive", c.cfg.Interactive),
	)
	if c.cfg.Interactive {
		fmt.Println("\n=== Interactive Mode ===")
		fmt.Println("Browser is visible. Solve CAPTCHAs or fill forms manually.")
		fmt.Println("You will be prompted on each page before content is captured.")
		fmt.Println("========================")
	}

	if c.cfg.ManualCapture {
		fmt.Println("\n=== Manual Capture Mode ===")
		fmt.Println("Navigate freely in the browser window.")
		fmt.Println("Each page you visit will be captured automatically.")
		fmt.Println("Press Ctrl+C or type 'q' + Enter to stop.")
		capCtx, _, err := c.browserPool.Acquire()
		if err != nil {
			return fmt.Errorf("failed to acquire browser for manual capture: %w", err)
		}
		c.manualCapture(capCtx, seeds)
		goto cleanup
	}

Loop:
	for c.urlQueue.Size() > 0 {
		select {
		case <-c.ctx.Done():
			break Loop
		default:
		}

		if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
			util.LogInfo("reached total URL limit", zap.Int("limit", c.cfg.MaxTotalURLs))
			break
		}

		item, ok := c.urlQueue.PopURL()
		if !ok {
			break
		}

		if item.Depth > c.cfg.MaxDepth {
			continue
		}

		memUsed := c.memoryBudget.Used()
		memMax := c.memoryBudget.Max()
		if memUsed > memMax*8/10 {
			runtime.GC()
		}

		host := getHost(item.URL)

		c.hostMu.Lock()
		if c.cfg.MaxURLsPerHost > 0 && c.hostURLCount[host] >= c.cfg.MaxURLsPerHost {
			c.hostMu.Unlock()
			continue
		}
		c.hostURLCount[host]++
		c.hostMu.Unlock()

		hostSemCh := c.getHostSem(host).ch
		select {
		case hostSemCh <- struct{}{}:
		case <-c.ctx.Done():
			continue
		}

		ua := cfgUserAgent(c.cfg)
		canCrawl, crawlDelay := c.robotsParser.CanCrawl(item.URL, ua, c.cfg)
		if !canCrawl {
			<-hostSemCh
			util.LogDebug("blocked by robots.txt", zap.String("url", item.URL))
			continue
		}

		startTime := time.Now()
		if err := c.rateLimiter.Wait(c.ctx, host, crawlDelay); err != nil {
			<-hostSemCh
			continue
		}

		if !c.circuitBreaker.Allow(host) {
			<-hostSemCh
			util.LogDebug("circuit open, skipping", zap.String("host", host))
			continue
		}

		c.wg.Add(1)
		select {
		case c.semaphore <- struct{}{}:
		case <-c.ctx.Done():
			<-hostSemCh
			c.wg.Done()
			break Loop
		}

		go func(url string, depth int, hst string, semCh chan struct{}, start time.Time) {
			defer c.wg.Done()
			defer func() { <-c.semaphore }()
			defer func() { <-semCh }()
			defer func() {
				latency := time.Since(start)
				c.rateLimiter.ObserveLatency(hst, latency)
			}()

			defer func() {
				if r := recover(); r != nil {
					buf := pool.GlobalBufferPool.Get()
					defer pool.GlobalBufferPool.Put(buf)
					n := runtime.Stack(buf.Bytes(), false)
					util.LogInfo("panic recovered",
						zap.String("url", url),
						zap.Any("error", r),
						zap.String("stack", buf.String()[:n]),
					)
					c.metrics.IncErrors()
				}
			}()

			c.crawlPage(c.ctx, url, depth)
		}(item.URL, item.Depth, host, hostSemCh, startTime)
	}

cleanup:
	drainTimer := time.NewTimer(drainTimeout)
	drainDone := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(drainDone)
	}()

	select {
	case <-drainDone:
		if !drainTimer.Stop() {
			<-drainTimer.C
		}
	case <-drainTimer.C:
		util.LogInfo("drain timeout reached, continuing")
	case <-c.ctx.Done():
		if !drainTimer.Stop() {
			<-drainTimer.C
		}
	}
	close(c.checkpointDone)

	c.storage.WriteIndex()
	c.writeJSErrors()
	c.writeWSMessages()

	apiResponses := c.apiResponses.GetAll()
	wsRaw := c.wsMessages.GetAll()
	c.writeAPIResponses(apiResponses)
	c.writeHAR(apiResponses)
	c.writeSW(apiResponses, wsRaw)
	c.writeSitemap()
	c.saveCookieJar()
	c.closeWriters()
	if c.persistentQueue != nil {
		c.persistentQueue.Save()
	}

	if c.cfg.BloomFilterPath != "" {
		c.bloomFilter.SaveToFile(c.cfg.BloomFilterPath)
	}
	if c.cfg.CheckpointFile != "" {
		c.saveCheckpoint()
	}
	if c.authManager != nil {
		c.authManager.Close()
	}

	pages, assets, errors, bytes := c.metrics.Snapshot()
	c.routeMu.RLock()
	routes := len(c.discoveredRoutes)
	c.routeMu.RUnlock()
	util.LogInfo("crawl completed",
		zap.Int64("pages", pages),
		zap.Int64("assets", assets),
		zap.Int64("errors", errors),
		zap.Int64("bytes", bytes),
		zap.Int("routes", routes),
	)

	if c.cfg.Incremental && c.incCache != nil {
		if err := c.incCache.Save(); err != nil {
			util.LogError("failed to save incremental cache", err)
		}
	}
	return nil
}

func (c *Crawler) reportProgress() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.checkpointDone:
			return
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			pages, assets, errors, bytes := c.metrics.Snapshot()
			util.LogInfo("progress",
				zap.Int64("pages", pages),
				zap.Int64("assets", assets),
				zap.Int64("errors", errors),
				zap.Int64("bytes", bytes),
				zap.Int("queue", c.urlQueue.Size()),
			)
		}
	}
}

func (c *Crawler) crawlPage(ctx context.Context, urlStr string, depth int) {
	c.totalURLs.Add(1)

	host := getHost(urlStr)
	util.LogInfo("crawling", zap.String("url", urlStr), zap.Int("depth", depth))

	var lastErr error
	for attempt := 0; attempt < c.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := c.retryConfig.GetBackoff(attempt - 1)
			util.LogInfo("retry",
				zap.String("url", urlStr),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-c.ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			}
		}

		tabCtx, tabRelease, err := c.browserPool.Acquire()
		if err != nil {
			util.LogError("failed to acquire browser from pool, skipping", err, zap.String("url", urlStr))
			return
		}

		lastErr = c.doCrawl(tabCtx, urlStr, depth)
		tabRelease()
		if lastErr == nil {
			c.circuitBreaker.Success(host)
			return
		}

		crawlErr := crawlerrors.Classify(lastErr)
		if !crawlErr.Retryable {
			break
		}
	}

	if lastErr != nil {
		c.circuitBreaker.Failure(host)
		util.LogError("failed", lastErr, zap.String("url", urlStr))
		c.metrics.IncErrors()
	}
}

func (c *Crawler) doCrawl(browserCtx context.Context, urlStr string, depth int) error {
	rawTabCtx, tabCancel := chromedp.NewContext(browserCtx)
	defer tabCancel()

	tabCtx, tabCancel2 := context.WithTimeout(rawTabCtx, c.cfg.PageTimeout)
	defer tabCancel2()

	c.setupMobileEmulation(tabCtx)

	if c.cfg.EnableStealth {
		if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(jsengine.StealthScript).Do(ctx)
			return err
		})); err != nil {
			util.LogDebug("stealth injection failed", zap.Error(err))
		}
	}

	if c.cfg.EnableRouteDiscovery {
		if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(jsengine.PushStateCaptureScript).Do(ctx)
			return err
		})); err != nil {
			util.LogDebug("pushstate capture injection failed", zap.Error(err))
		}
	}

	c.setupConsoleCapture(tabCtx)
	c.setupWSCapture(tabCtx)
	c.setCookies(tabCtx)
	c.injectPersistedCookies(tabCtx, urlStr)

	if len(c.cfg.BlockedURLPatterns) > 0 {
		if err := chromedp.Run(tabCtx, network.SetBlockedURLS(c.cfg.BlockedURLPatterns)); err != nil {
			util.LogDebug("failed to set blocked URL patterns", zap.Error(err))
		}
	}

	if !c.cfg.Interactive && c.authManager != nil && c.cfg.AuthConfig != nil && c.cfg.AuthConfig.Enabled {
		if err := c.authManager.Authenticate(tabCtx, urlStr); err != nil {
			util.LogError("authentication failed", err, zap.String("url", urlStr))
		}
	}

	workerCount := c.cfg.MaxConcurrentPages * 2
	if workerCount < 5 {
		workerCount = 5
	}
	netIntercept := netintercept.NewInterceptorWithWorkers(workerCount)
	defer netIntercept.Close()
	netIntercept.SetAPICallback(func(ar netintercept.APIResponse) {
		if !c.cfg.EnableAPICapture {
			return
		}
		if !apiURLMatches(ar.URL, c.cfg.InterceptAPIs) {
			return
		}

		var reqBody []byte
		if ar.Request != nil && len(ar.Request.Body) > 0 {
			reqBody = ar.Request.Body
		}

		gqlOp := extractGraphQLOp(reqBody)

		c.apiResponses.Push(CapturedAPIResponse{
			URL:         ar.URL,
			Method:      ar.Method,
			StatusCode:  ar.StatusCode,
			Body:        ar.Body,
			Headers:     ar.Headers,
			RequestBody: reqBody,
			Timestamp:   time.Now(),
			Size:        len(ar.Body),
			GraphQLOp:   gqlOp,
		})
		c.writeRecord(&storage.WARCRecord{
			URL:        ar.URL,
			Body:       ar.Body,
			MimeType:   "application/json",
			Date:       time.Now(),
			StatusCode: ar.StatusCode,
			RecordType: "response",
			ContentLen: int64(len(ar.Body)),
		})
		savePath := c.storage.PathForAPI(ar.URL)
		if savePath != "" {
			os.MkdirAll(filepath.Dir(savePath), 0755)
			if err := os.WriteFile(savePath, ar.Body, 0644); err != nil {
				util.LogDebug("failed to save API response", zap.Error(err))
			}
		}
	})
	netIntercept.Start(tabCtx, urlStr)

	for _, js := range c.cfg.JSBeforeLoad {
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, nil)); err != nil {
			util.LogDebug("js before load failed", zap.Error(err))
		}
	}

	err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errorText, err := page.Navigate(urlStr).Do(ctx)
		if err != nil {
			return err
		}
		if errorText != "" {
			return crawlerrors.Wrap(crawlerrors.KindBrowser, "navigation failed", fmt.Errorf("%s", errorText))
		}
		return nil
	}))
	if err != nil {
		return err
	}

	c.waitForPage(tabCtx)

	var finalURL string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(`window.location.href`, &finalURL)); err != nil {
		util.LogDebug("failed to get final URL", zap.Error(err))
	}
	if finalURL != "" && finalURL != urlStr {
		util.LogDebug("redirected",
			zap.String("from", urlStr),
			zap.String("to", finalURL),
		)
	}
	c.persistCookies(tabCtx, urlStr)

	c.applyWaitStrategy(tabCtx)

	if c.cfg.DismissOverlays {
		jsengine.DismissOverlays(tabCtx)
	}
	if c.cfg.ExpandSections {
		jsengine.ExpandAllSections(tabCtx)
	}
	for _, selector := range c.cfg.ClickSelectors {
		jsengine.ClickElement(tabCtx, selector)
		clickTimer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-clickTimer.C:
		case <-c.ctx.Done():
			if !clickTimer.Stop() {
				<-clickTimer.C
			}
			return nil
		}
	}
	if c.cfg.EnableLazyLoad {
		jsengine.InjectLazyLoad(tabCtx)
	}

	if c.cfg.InfiniteScroll != nil && c.cfg.InfiniteScroll.Enabled {
		scrollCfg := &jsengine.InfiniteScrollConfig{
			Enabled:          c.cfg.InfiniteScroll.Enabled,
			MaxScrolls:       c.cfg.InfiniteScroll.MaxScrolls,
			MaxDuration:      c.cfg.InfiniteScroll.MaxDuration,
			StablePasses:     c.cfg.InfiniteScroll.StablePasses,
			ItemSelector:     c.cfg.InfiniteScroll.ItemSelector,
			ScrollContainer:  c.cfg.InfiniteScroll.ScrollContainer,
			LoadMoreSelector: c.cfg.InfiniteScroll.LoadMoreSelector,
			ScrollDelay:      c.cfg.InfiniteScroll.ScrollDelay,
			ScrollDistance:   c.cfg.InfiniteScroll.ScrollDistance,
		}
		jsengine.InfiniteScroll(tabCtx, scrollCfg)
	}

	for _, js := range c.cfg.JSAfterLoad {
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, nil)); err != nil {
			util.LogDebug("js after load failed", zap.Error(err))
		}
	}

	if c.cfg.Interactive {
		c.promptUser(tabCtx, urlStr)
	}

	// Systematic interaction engine - click all links, fill forms, etc.
	if c.cfg.EnableInteractionEngine {
		c.runInteractionEngine(tabCtx, urlStr)
	}

	if c.cfg.EnableRouteDiscovery {
		routeInfo, err := jsengine.DiscoverRoutes(tabCtx)
		if err == nil && routeInfo != nil {
			c.routeMu.Lock()
			for _, route := range routeInfo.Routes {
				if route != "" {
					absURL := rewrite.ResolveURL(urlStr, route)
					if absURL != "" {
						c.discoveredRoutes[absURL] = true
					}
				}
			}
			c.routeMu.Unlock()
		}

		// Collect pushState/hashchange/popstate routes
		pushRoutes, err := jsengine.GetPushStateRoutes(tabCtx)
		if err == nil && len(pushRoutes) > 0 {
			routesToCrawl := make([]string, 0, len(pushRoutes))
			c.routeMu.Lock()
			for _, pr := range pushRoutes {
				absURL := rewrite.ResolveURL(urlStr, pr.URL)
				if absURL != "" {
					if !c.discoveredRoutes[absURL] {
						c.discoveredRoutes[absURL] = true
						routesToCrawl = append(routesToCrawl, absURL)
					}
				}
			}
			c.routeMu.Unlock()

			util.LogDebug("discovered pushState routes",
				zap.String("url", urlStr),
				zap.Int("count", len(pushRoutes)),
				zap.Int("new_routes", len(routesToCrawl)),
			)

			// Navigate to discovered SPA routes to capture dynamically rendered content
			if c.cfg.MaxSPARoutes > 0 && len(routesToCrawl) > c.cfg.MaxSPARoutes {
				routesToCrawl = routesToCrawl[:c.cfg.MaxSPARoutes]
			}
		routeLoop:
			for _, routeURL := range routesToCrawl {
				select {
				case <-c.ctx.Done():
					break routeLoop
				default:
				}
				util.LogDebug("navigating to SPA route", zap.String("route", routeURL))
				navCtx, navCancel := context.WithTimeout(tabCtx, 30*time.Second)
				err := chromedp.Run(navCtx,
					chromedp.ActionFunc(func(ctx context.Context) error {
						_, _, errorText, err := page.Navigate(routeURL).Do(ctx)
						if err != nil {
							return err
						}
						if errorText != "" {
							return fmt.Errorf("navigation error: %s", errorText)
						}
						return nil
					}),
				)
				navCancel()
				if err != nil {
					util.LogDebug("failed to navigate to SPA route",
						zap.String("route", routeURL),
						zap.Error(err),
					)
					continue
				}

				// Wait for SPA to hydrate and render
				waitCtx, waitCancel := context.WithTimeout(tabCtx, 15*time.Second)
				chromedp.Run(waitCtx, chromedp.WaitReady("body", chromedp.ByQuery))
				// Wait for network idle after SPA navigation
				jsengine.WaitForNetworkIdle(waitCtx, 1*time.Second)
				// Additional wait for dynamic content
				chromedp.Sleep(2 * time.Second).Do(waitCtx)
				waitCancel()

				// Capture the rendered HTML
				var spaHTML string
				if err := chromedp.Run(tabCtx, chromedp.OuterHTML("html", &spaHTML)); err != nil || spaHTML == "" {
					continue
				}

				// Save the SPA route content
				spaPath, err := c.storage.SaveHTML(routeURL, []byte(spaHTML))
				if err != nil {
					util.LogDebug("failed to save SPA route HTML", zap.Error(err))
					continue
				}
				c.rewriter.AddMapping(routeURL, spaPath)
				util.LogDebug("captured SPA route", zap.String("route", routeURL), zap.String("path", spaPath))
			}
		}
	}

	if c.cfg.EnableShadowDOM {
		shadowInfo, err := jsengine.ExtractShadowDOM(tabCtx)
		if err == nil && shadowInfo != nil && shadowInfo.Count > 0 {
			data, err := json.Marshal(shadowInfo)
			if err != nil {
				util.LogError("failed to marshal shadow DOM", err)
			} else {
				c.storage.SaveShadowDOM(urlStr, data)
			}
		}
	}

	var structuredData []byte
	if c.cfg.EnableStructuredData {
		sd, err := jsengine.ExtractStructuredData(tabCtx)
		if err == nil && sd != nil && (len(sd.JSONLD) > 0 || len(sd.OG) > 0 || len(sd.Twitter) > 0 || len(sd.Meta) > 0) {
			structuredData, err = json.Marshal(sd)
			if err == nil {
				savePath := c.cfg.OutputDir + "/" + getHost(urlStr) + "/structured-data.json"
				os.MkdirAll(filepath.Dir(savePath), 0755)
				if err := os.WriteFile(savePath, structuredData, 0644); err != nil {
					util.LogDebug("failed to save structured data", zap.Error(err))
				}
			}
		}
	}

	if c.cfg.EnableStealth {
		swMgr := jsengine.NewServiceWorkerManager()
		swMgr.Detect(tabCtx)
		swMgr.Unregister(tabCtx)
	}

	if c.cfg.EnableArticleExtract {
		article, err := jsengine.ExtractArticle(tabCtx)
		if err == nil && article != nil && article.Content != "" {
			articlePath := c.cfg.OutputDir + "/" + getHost(urlStr) + "/article.json"
			os.MkdirAll(filepath.Dir(articlePath), 0755)
			article.URL = urlStr
			article.ExtractedAt = time.Now().Format(time.RFC3339)
			articleData, _ := json.MarshalIndent(article, "", "  ")
			if err := os.WriteFile(articlePath, articleData, 0644); err != nil {
				util.LogDebug("failed to save article", zap.Error(err))
			}
		}
	}

	if c.cfg.EnableSingleFile {
		singleFile, err := jsengine.GenerateSingleFile(tabCtx)
		if err == nil && singleFile != "" {
			singleFilePath := c.cfg.OutputDir + "/" + getHost(urlStr) + "/singlefile.html"
			os.MkdirAll(filepath.Dir(singleFilePath), 0755)
			if err := os.WriteFile(singleFilePath, []byte(singleFile), 0644); err != nil {
				util.LogError("failed to save single file", err)
			}
		}
	}

	html, err := c.captureCurrentPage(rawTabCtx, urlStr, netIntercept)
	if err != nil {
		return err
	}

	links := c.rewriter.ExtractLinks(urlStr, []byte(html))
	c.routeMu.RLock()
	for route := range c.discoveredRoutes {
		if c.isSameDomain(urlStr, route) {
			links = append(links, route)
		}
	}
	c.routeMu.RUnlock()

	for _, link := range links {
		normalized := c.normalizeURL(queue.NormalizeAndClean(link))
		if c.shouldQueue(normalized) {
			c.urlQueue.PushURL(normalized, depth+1)
		}
	}

	// Extract and queue iframe sources
	if c.cfg.EnableIframes && depth < c.cfg.MaxIframeDepth {
		iframeSources, err := jsengine.ExtractIframeSources(tabCtx)
		if err == nil && len(iframeSources) > 0 {
			util.LogDebug("discovered iframes",
				zap.String("url", urlStr),
				zap.Int("count", len(iframeSources)),
			)
			for _, iframe := range iframeSources {
				if iframe.Src == "" {
					continue
				}
				absURL := rewrite.ResolveURL(urlStr, iframe.Src)
				if absURL == "" {
					continue
				}
				normalized := c.normalizeURL(queue.NormalizeAndClean(absURL))
				if c.shouldQueue(normalized) {
					c.urlQueue.PushURL(normalized, depth+1)
				}
			}
		}
	}

	// Extract and queue media sources (video/audio)
	if c.cfg.EnableMediaCapture {
		mediaSources, err := jsengine.ExtractMediaSources(tabCtx)
		if err == nil && len(mediaSources) > 0 {
			util.LogDebug("discovered media sources",
				zap.String("url", urlStr),
				zap.Int("count", len(mediaSources)),
			)
			for _, ms := range mediaSources {
				if ms.Src == "" {
					continue
				}
				absURL := rewrite.ResolveURL(urlStr, ms.Src)
				if absURL == "" {
					continue
				}
				normalized := c.normalizeURL(queue.NormalizeAndClean(absURL))
				if c.shouldQueue(normalized) {
					c.urlQueue.PushURL(normalized, depth+1)
				}

				if ms.Poster != "" {
					posterURL := rewrite.ResolveURL(urlStr, ms.Poster)
					if posterURL != "" {
						pNorm := c.normalizeURL(queue.NormalizeAndClean(posterURL))
						if c.shouldQueue(pNorm) {
							c.urlQueue.PushURL(pNorm, depth+1)
						}
					}
				}
			}
		}
	}

	pageCtx, pageCancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer pageCancel()
	c.fetchPageMetadata(pageCtx, urlStr)

	netIntercept.Close()

	return nil
}
