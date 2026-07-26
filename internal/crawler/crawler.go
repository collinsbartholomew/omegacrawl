package crawler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
	"go.uber.org/zap"

	"github.com/user/clone/internal/auth"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/changedetection"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/jsanalyzer"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/robots"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"

	"github.com/cespare/xxhash/v2"
	clientpool "github.com/user/clone/internal/httpclient"
	crawlerrors "github.com/user/clone/internal/errors"
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
	URL           string            `json:"url"`
	Method        string            `json:"method"`
	StatusCode    int               `json:"status_code"`
	Body          []byte            `json:"body"`
	Headers       map[string]string `json:"headers"`
	RequestBody   []byte            `json:"request_body,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Size          int               `json:"size"`
	GraphQLOp     string            `json:"graphql_op,omitempty"`
}

const maxContentHashes = 200000
const maxJSErrors = 10000
const maxWSMessages = 5000
const maxAPICaptures = 2000
const maxQueueSize = 100000
const drainTimeout = 30 * time.Second

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

type Crawler struct {
	cfg                *config.Config
	storage            *storage.Filesystem
	warc               *storage.WARCWriter

	hostSemaphores     map[string]*hostSem
	exactDedup         *util.LRUSet
	contentHashes      *util.LRUSet
	discoveredRoutes   map[string]bool
	jsErrors           *util.BoundedQueue
	wsMessages         *util.BoundedQueue
	apiResponses       *util.BoundedQueue

	browserMu          sync.Mutex
	browserCancel      context.CancelFunc

	robotsParser       *robots.RobotsParser
	rewriter           *rewrite.Rewriter
	urlQueue        queue.Queue
	persistentQueue *queue.PersistentQueue
	bloomFilter        *queue.BloomDedup
	rateLimiter        *ratelimit.RateLimiter
	circuitBreaker     *resilience.HostCircuitBreaker
	retryConfig        *RetryConfig
	checkpoint         *Checkpoint
	semaphore          chan struct{}
	httpClient         *http.Client
	wg                 sync.WaitGroup
	ctx                context.Context
	cancel             context.CancelFunc
	hostLastCrawl      map[string]time.Time
	hostURLCount       map[string]int
	hostMu             sync.RWMutex
	routeMu            sync.RWMutex
	totalURLs          atomic.Int64
	checkpointDone     chan struct{}
	shutdown           atomic.Bool

	browserCtx         context.Context
	allocOpts          []chromedp.ExecAllocatorOption

	metrics            *util.Metrics
	incCache           *storage.ResourceCache

	cookieJar          map[string][]*http.Cookie
	cookieMu           sync.RWMutex

	wsURLs             map[network.RequestID]string
	wsMu               sync.RWMutex

	authManager        *auth.AuthManager
	changeDetector    *changedetection.Detector
	captchaSolver     *captcha.Solver

}



type hostSem struct {
	ch       chan struct{}
	closed   atomic.Bool
}

type BrowserPool struct {
	browsers   chan context.Context
	workers    int
}

func NewBrowserPool(workSize int) *BrowserPool {
	browsers := make(chan context.Context, workSize)
	return &BrowserPool{
		browsers: browsers,
		workers:  workSize,
	}
}

func (bp *BrowserPool) GetContext() context.Context {
	return <-bp.browsers
}

func (bp *BrowserPool) ReleaseContext(ctx context.Context) {
	bp.browsers <- ctx
}

func (bp *BrowserPool) Close() {
	close(bp.browsers)
}

func NewCrawler(cfg *config.Config) (*Crawler, error) {
	store := storage.NewFilesystem(cfg)
	robotsParser := robots.NewRobotsParser()
	robotsParser.SetUserAgent(cfg.UserAgent)
	rw := rewrite.NewRewriter()

	ctx, cancel := context.WithCancel(context.Background())

	var bloomFilter *queue.BloomDedup
	expectedItems := uint(cfg.MaxTotalURLs)
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
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)

	urlQueue, err := queue.NewQueueFromConfig(cfg.QueueConfig)
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
		urlQueue:        urlQueue,
		persistentQueue: persistentQueue,
		bloomFilter:      bloomFilter,
		exactDedup:       util.NewLRUSet(lruSize),
		rateLimiter:      ratelimit.New(cfg.CrawlDelay, 1),
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

	c.authManager = auth.NewAuthManager(c.cfg.AuthConfig)

	if cfg.EnableWARC {
		c.warc = storage.NewWARCWriter(cfg.OutputDir)
	}

	if cfg.ChangeDetectionConfig != nil && cfg.ChangeDetectionConfig.Enabled {
		snapDir := cfg.ChangeDetectionConfig.SnapshotDir
		if snapDir == "" {
			snapDir = cfg.OutputDir + "/snapshots"
		}
		c.changeDetector = changedetection.NewDetector(snapDir)
	}

	if cfg.CAPTCHAConfig != nil && cfg.CAPTCHAConfig.Enabled {
		c.captchaSolver = captcha.NewSolver(cfg.CAPTCHAConfig)
	}

	return c, nil
}

func (c *Crawler) Stop() {
	c.shutdown.Store(true)
	c.cancel()
	c.rateLimiter.Stop()
}

func (c *Crawler) Start(seeds []string) error {
	defer c.cancel()

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
		chromedp.Flag("headless", true),
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

	if err := c.launchBrowser(); err != nil {
		return err
	}
	browserCtx := c.getBrowserCtx()

	go c.periodicCheckpoint()
	go c.reportProgress()
	go c.periodicCleanup()

	c.loadCookieJar()

	util.LogInfo("starting crawl",
		zap.Int("seeds", len(seeds)),
		zap.Int("max_concurrent", c.cfg.MaxConcurrentPages),
		zap.Int("max_depth", c.cfg.MaxDepth),
		zap.Int("max_urls_per_host", c.cfg.MaxURLsPerHost),
		zap.String("wait_strategy", c.cfg.WaitStrategy),
		zap.Bool("infinite_scroll", c.cfg.InfiniteScroll.Enabled),
		zap.Bool("stealth", c.cfg.EnableStealth),
		zap.Bool("respect_robots", c.cfg.RespectRobots),
	)

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

		host := getHost(item.URL)

		c.hostMu.Lock()
		if c.cfg.MaxURLsPerHost > 0 && c.hostURLCount[host] >= c.cfg.MaxURLsPerHost {
			c.hostMu.Unlock()
			continue
		}
		c.hostURLCount[host]++
		c.hostMu.Unlock()

hs := c.getHostSem(host)
	if hs.closed.Load() {
		hs = &hostSem{ch: make(chan struct{}, 2)}
	}
	select {
	case hs.ch <- struct{}{}:
	case <-c.ctx.Done():
		continue
	}

	ua := cfgUserAgent(c.cfg)
	canCrawl, crawlDelay := c.robotsParser.CanCrawl(item.URL, ua, c.cfg)
	if !canCrawl {
		<-hs.ch
		util.LogDebug("blocked by robots.txt", zap.String("url", item.URL))
		continue
	}

	startTime := time.Now()
	c.rateLimiter.Wait(c.ctx, host, crawlDelay)

	if !c.circuitBreaker.Allow(host) {
		<-hs.ch
		util.LogDebug("circuit open, skipping", zap.String("host", host))
		continue
	}

	c.wg.Add(1)
	select {
	case c.semaphore <- struct{}{}:
	case <-c.ctx.Done():
		<-hs.ch
		c.wg.Done()
		break Loop
	}

	go func(url string, depth int, hst string, start time.Time) {
		defer c.wg.Done()
		defer func() { <-c.semaphore }()
		defer func() { <-c.getHostSem(hst).ch }()
		defer func() {
			latency := time.Since(start)
			c.rateLimiter.ObserveLatency(hst, latency)
		}()

			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					util.LogInfo("panic recovered",
						zap.String("url", url),
						zap.Any("error", r),
						zap.String("stack", string(buf[:n])),
					)
					c.metrics.IncErrors()
				}
			}()

			c.crawlPage(browserCtx, url, depth)
		}(item.URL, item.Depth, host, startTime)
	}

	drainTimer := time.NewTimer(drainTimeout)
	drainDone := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(drainDone)
	}()

	select {
	case <-drainDone:
		drainTimer.Stop()
	case <-drainTimer.C:
		util.LogInfo("drain timeout reached, continuing")
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
	if c.warc != nil {
		c.warc.Close()
	}
	if c.persistentQueue != nil {
		c.persistentQueue.Save()
	}

	if c.cfg.BloomFilterPath != "" {
		c.bloomFilter.SaveToFile(c.cfg.BloomFilterPath)
	}
	if c.cfg.CheckpointFile != "" {
		c.saveCheckpoint()
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

func (c *Crawler) writeJSErrors() {
	errs := c.jsErrors.GetAll()
	if len(errs) == 0 {
		return
	}
	jsErrors := make([]JSError, 0, len(errs))
	for _, e := range errs {
		if je, ok := e.(JSError); ok {
			jsErrors = append(jsErrors, je)
		}
	}
	if len(jsErrors) == 0 {
		return
	}
	data, err := json.MarshalIndent(jsErrors, "", "  ")
	if err != nil {
		util.LogError("failed to marshal JS errors", err)
		return
	}
	path := c.cfg.OutputDir + "/js-errors.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write JS errors", err)
		return
	}
	util.LogInfo("wrote JS errors", zap.String("path", path), zap.Int("count", len(jsErrors)))
}

func (c *Crawler) writeWSMessages() {
	msgs := c.wsMessages.GetAll()
	if len(msgs) == 0 {
		return
	}
	wsMessages := make([]WSMsg, 0, len(msgs))
	for _, e := range msgs {
		if wm, ok := e.(WSMsg); ok {
			wsMessages = append(wsMessages, wm)
		}
	}
	if len(wsMessages) == 0 {
		return
	}
	data, err := json.MarshalIndent(wsMessages, "", "  ")
	if err != nil {
		util.LogError("failed to marshal WS messages", err)
		return
	}
	path := c.cfg.OutputDir + "/ws-messages.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write WS messages", err)
		return
	}
	util.LogInfo("wrote WS messages", zap.String("path", path), zap.Int("count", len(wsMessages)))
}

func (c *Crawler) writeAPIResponses(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}
	data, err := json.MarshalIndent(apiResp, "", "  ")
	if err != nil {
		util.LogError("failed to marshal API responses", err)
		return
	}
	path := c.cfg.OutputDir + "/api-responses.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write API responses", err)
		return
	}
	util.LogInfo("wrote API responses", zap.String("path", path), zap.Int("count", len(apiResp)))
}

type harEntry struct {
	StartedDateTime string `json:"startedDateTime"`
	Time            int    `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}

type harRequest struct {
	Method      string     `json:"method"`
	URL         string     `json:"url"`
	HTTPVersion string     `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	Cookies     []harNameValue `json:"cookies"`
	HeadersSize int        `json:"headersSize"`
	BodySize    int        `json:"bodySize"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	Cookies     []harNameValue `json:"cookies"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harFile struct {
	Log harLog `json:"log"`
}

func (c *Crawler) writeHAR(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}

	entries := make([]harEntry, 0, len(apiResp))
	for _, a := range apiResp {
		headers := make([]harNameValue, 0, len(a.Headers))
		for k, v := range a.Headers {
			headers = append(headers, harNameValue{Name: k, Value: v})
		}
		var cookies []harNameValue
		for k, v := range a.Headers {
			if strings.ToLower(k) == "set-cookie" || strings.ToLower(k) == "cookie" {
				cookies = append(cookies, harNameValue{Name: k, Value: v})
			}
		}
		if cookies == nil {
			cookies = []harNameValue{}
		}

		statusText := http.StatusText(a.StatusCode)
		if statusText == "" {
			statusText = "Unknown"
		}

		contentType := "application/octet-stream"
		if ct, ok := a.Headers["Content-Type"]; ok {
			contentType = ct
		} else if ct, ok := a.Headers["content-type"]; ok {
			contentType = ct
		}

		mimeType := contentType
		if idx := strings.IndexByte(contentType, ';'); idx != -1 {
			mimeType = strings.TrimSpace(contentType[:idx])
		}

		bodySize := a.Size
		headersSize := 0
		for _, h := range headers {
			headersSize += len(h.Name) + len(h.Value) + 4
		}

		entries = append(entries, harEntry{
			StartedDateTime: a.Timestamp.Format(time.RFC3339),
			Time:            -1,
			Request: harRequest{
				Method:      a.Method,
				URL:         a.URL,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				QueryString: []harNameValue{},
				Cookies:     cookies,
				HeadersSize: headersSize,
				BodySize:    bodySize,
			},
			Response: harResponse{
				Status:      a.StatusCode,
				StatusText:  statusText,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				Cookies:     cookies,
			Content: harContent{
				Size:     bodySize,
				MimeType: mimeType,
				Text:     string(a.Body),
			},
				RedirectURL: "",
				HeadersSize: headersSize,
				BodySize:    bodySize,
			},
			Cache: struct{}{},
			Timings: harTimings{
				Send:    -1,
				Wait:    -1,
				Receive: -1,
			},
		})
	}

	har := harFile{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{
				Name:    "clone",
				Version: "1.0",
			},
			Entries: entries,
		},
	}

	data, err := json.MarshalIndent(har, "", "  ")
	if err != nil {
		util.LogError("failed to marshal HAR", err)
		return
	}

	path := c.cfg.OutputDir + "/api-responses.har"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write HAR", err)
		return
	}
	util.LogInfo("wrote HAR", zap.String("path", path), zap.Int("count", len(entries)))
}

func (c *Crawler) writeSW(responses []interface{}, wsRaw []interface{}) {
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}

	wsByURL := make(map[string][]WSMsg)
	for _, w := range wsRaw {
		if m, ok := w.(WSMsg); ok && m.URL != "" {
			wsByURL[m.URL] = append(wsByURL[m.URL], m)
		}
	}
	wsJSON, _ := json.Marshal(wsByURL)

	urlMappings := c.rewriter.GetMappings()
	urlMapRel := make(map[string]string, len(urlMappings))
	outPrefix := c.cfg.OutputDir
	if !strings.HasSuffix(outPrefix, "/") {
		outPrefix += "/"
	}
	for origURL, localPath := range urlMappings {
		rel := strings.TrimPrefix(localPath, outPrefix)
		rel = filepath.ToSlash(rel)
		urlMapRel[origURL] = rel
	}
	urlMapJSON, _ := json.Marshal(urlMapRel)

	var b strings.Builder
	b.WriteString(`const CACHE = 'clone-v1';
const API_MAP = {`)
	for i, a := range apiResp {
		if i > 0 {
			b.WriteString(`,`)
		}
		bodyJSON, _ := json.Marshal(string(a.Body))
		reqBodyJSON, _ := json.Marshal(string(a.RequestBody))
		gqlKey := ""
		if a.GraphQLOp != "" {
			gqlKey = a.URL + "|gql:" + a.GraphQLOp
			gqlKeyJSON, _ := json.Marshal(gqlKey)
			b.WriteString(fmt.Sprintf(`%s:{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s,"gql":%s}`,
				string(gqlKeyJSON), a.Method, a.StatusCode, jsonHeadrs(a.Headers), string(bodyJSON), string(reqBodyJSON), string(bodyJSON)))
			b.WriteString(`,`)
		}
		b.WriteString(fmt.Sprintf(`"%s":{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s}`,
			a.URL, a.Method, a.StatusCode, jsonHeadrs(a.Headers), string(bodyJSON), string(reqBodyJSON)))
	}
	b.WriteString(`};
const WS_MAP = `)
	b.Write(wsJSON)
	b.WriteString(`;
const URL_MAP = `)
	b.Write(urlMapJSON)
	b.WriteString(`;
const STATIC_EXT = /\.(css|js|png|jpg|jpeg|gif|svg|ico|woff2?|ttf|eot|webp)(\?.*)?$/;
const API_PATTERN = /\/api\//;

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE).then(function(cache) {
      var urls = Object.keys(URL_MAP).map(function(url) { return URL_MAP[url]; });
      return Promise.all(
        urls.map(function(path) {
          return cache.add(path).catch(function() {});
        })
      );
    }).then(function() {
      self.skipWaiting();
    })
  );
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys =>
    Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
  ));
  return self.clients.claim();
});

function matchAPI(url, method, body) {
  if (isGraphQL(url) && body) {
    try {
      var parsed = JSON.parse(body);
      if (parsed.operationName) {
        var gqlKey = url + '|gql:' + parsed.operationName;
        var gqlEntry = API_MAP[gqlKey];
        if (gqlEntry && (!gqlEntry.m || gqlEntry.m === method)) return gqlEntry;
      }
    } catch(e) {}
  }
  const entry = API_MAP[url];
  if (entry && (!entry.m || entry.m === method)) return entry;
  const noQuery = url.split('?')[0].split('#')[0];
  if (noQuery !== url) {
    const e2 = API_MAP[noQuery];
    if (e2 && (!e2.m || e2.m === method)) return e2;
  }
  for (const [key, val] of Object.entries(API_MAP)) {
    if (key.includes('|gql:')) continue;
    const base = key.split('?')[0];
    if (base === noQuery && (!val.m || val.m === method)) return val;
  }
  return null;
}

function isGraphQL(url) {
  return url.includes('/graphql') || url.includes('/gql');
}

function getWSMessages(url) {
  const normalized = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
  let msgs = WS_MAP[url] || WS_MAP[normalized];
  if (msgs) return msgs;
  for (const [key, val] of Object.entries(WS_MAP)) {
    if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === normalized) return val;
  }
  return null;
}

function matchURL(url) {
  const withoutQuery = url.split('?')[0].split('#')[0];
  if (URL_MAP[url]) return URL_MAP[url];
  if (URL_MAP[withoutQuery]) return URL_MAP[withoutQuery];
  for (const [key, val] of Object.entries(URL_MAP)) {
    if (key.split('?')[0].split('#')[0] === withoutQuery) return val;
  }
  return null;
}

function isHTML(url) {
  return !STATIC_EXT.test(url) && !API_PATTERN.test(url) && !url.includes('/api/');
}

self.addEventListener('fetch', event => {
  const { request } = event;
  const url = request.url;

  if (API_PATTERN.test(url) || isGraphQL(url)) {
    if (isGraphQL(url) && (request.method === 'POST' || request.method === 'PUT')) {
      event.respondWith(
        request.clone().text().then(function(body) {
          var entry = matchAPI(url, request.method, body);
          if (entry) {
            if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
              var replay = new Request(url, {
                method: request.method,
                headers: request.headers,
                body: entry.rb
              });
              return fetch(replay)['catch'](function() {
                return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
              });
            }
            return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
          }
          return fetch(request)['catch'](function() {
            return new Response('', { status: 503 });
          });
        })
      );
      return;
    }
    const entry = matchAPI(url, request.method, null);
    if (entry) {
      if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
        const replay = new Request(url, {
          method: request.method,
          headers: request.headers,
          body: entry.rb
        });
        event.respondWith(
          fetch(replay).catch(() => new Response(entry.b, {
            status: entry.s,
            statusText: 'OK',
            headers: entry.h
          }))
        );
        return;
      }
      event.respondWith(new Response(entry.b, {
        status: entry.s,
        statusText: 'OK',
        headers: entry.h
      }));
      return;
    }
  }

  if (STATIC_EXT.test(url)) {
    event.respondWith(
      caches.match(request).then(cached => {
        if (cached) return cached;
        return fetch(request).then(res => {
          const copy = res.clone();
          caches.open(CACHE).then(cache => cache.put(request, copy));
          return res;
        }).catch(async () => {
          const localPath = matchURL(url);
          if (localPath) {
            const cached = await caches.match(localPath);
            if (cached) return cached;
          }
          return new Response('', { status: 404 });
        });
      })
    );
    return;
  }

  if (isHTML(url)) {
    event.respondWith(
      fetch(request).then(res => {
        const copy = res.clone();
        caches.open(CACHE).then(cache => {
          if (copy.ok && copy.headers.get('content-type')?.includes('text/html')) {
            cache.put(request, copy);
          }
        });
        return res;
      }).catch(async () => {
        const cached = await caches.match(request);
        if (cached) return cached;
        const localPath = matchURL(url);
        if (localPath) {
          const localCached = await caches.match(localPath);
          if (localCached) return localCached;
          return fetch(localPath).catch(() => caches.match('/offline.html'));
        }
        return new Response('Offline', { status: 503 });
      })
    );
    return;
  }

  event.respondWith(fetch(request));
});`)

	path := c.cfg.OutputDir + "/sw.js"
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		util.LogError("failed to write sw.js", err)
		return
	}
	util.LogInfo("wrote sw.js", zap.String("path", path), zap.Int("api_count", len(apiResp)))

	wsReplayScript := `(function() {
	var wsData = null;
	fetch('ws-data.json').then(function(r) { return r.json(); }).then(function(data) { wsData = data; }).catch(function() {});
	var NativeWebSocket = window.WebSocket;
	function findMessages(url) {
		if (!wsData) return null;
		var httpURL = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
		if (wsData[httpURL]) return wsData[httpURL];
		if (wsData[url]) return wsData[url];
		for (var key in wsData) {
			if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === httpURL) return wsData[key];
		}
		return null;
	}
	function decodeData(msg) {
		if (msg.is_binary) {
			var binaryStr = atob(msg.data);
			var bytes = new Uint8Array(binaryStr.length);
			for (var i = 0; i < binaryStr.length; i++) {
				bytes[i] = binaryStr.charCodeAt(i);
			}
			return bytes.buffer;
		}
		return msg.data;
	}
	window.WebSocket = function(url, protocols) {
		var msgs = findMessages(url);
		if (msgs) {
			var ws = {
				url: url, readyState: 0,
				CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3,
				onopen: null, onclose: null, onmessage: null, onerror: null,
				bufferedAmount: 0, extensions: '', protocol: protocols || '',
				close: function() { this.readyState = 3; if (this.onclose) this.onclose({ code: 1000, reason: 'Replay complete', wasClean: true }); },
				send: function(data) {},
				addEventListener: function(type, listener) {
					if (type === 'open') this.onopen = listener;
					else if (type === 'message') this.onmessage = listener;
					else if (type === 'close') this.onclose = listener;
					else if (type === 'error') this.onerror = listener;
				}
			};
			ws.readyState = 0;
			setTimeout(function() {
				ws.readyState = 1;
				if (ws.onopen) ws.onopen({ target: ws });
				var receives = [];
				var timestamps = [];
				var baseTime = 0;
				for (var i = 0; i < msgs.length; i++) {
					if (msgs[i].direction === 'receive') {
						receives.push(msgs[i]);
						timestamps.push(msgs[i].timestamp ? new Date(msgs[i].timestamp).getTime() : 0);
					}
				}
				if (timestamps.length > 0 && timestamps[0] > 0) {
					baseTime = timestamps[0];
				}
				for (var i = 0; i < receives.length; i++) {
					(function(idx, msg) {
						var delay = 50;
						if (baseTime > 0 && timestamps[idx] > 0) {
							delay = timestamps[idx] - baseTime;
						} else {
							delay = idx * 50;
						}
						setTimeout(function() {
							if (ws.onmessage) ws.onmessage({ data: decodeData(msg), target: ws, type: 'message' });
						}, delay);
					})(i, receives[i]);
				}
				var lastDelay = receives.length > 0 ? (timestamps[receives.length-1] > 0 ? timestamps[receives.length-1] - baseTime : receives.length * 50) + 100 : 100;
				setTimeout(function() {
					ws.readyState = 3;
					if (ws.onclose) ws.onclose({ code: 1000, reason: 'Replay complete', wasClean: true });
				}, Math.max(lastDelay, 100));
			}, 100);
			return ws;
		}
		return new NativeWebSocket(url, protocols);
	};
	window.WebSocket.CONNECTING = 0;
	window.WebSocket.OPEN = 1;
	window.WebSocket.CLOSING = 2;
	window.WebSocket.CLOSED = 3;
})();`

	wsReplayPath := c.cfg.OutputDir + "/ws-replay.js"
	if err := os.WriteFile(wsReplayPath, []byte(wsReplayScript), 0644); err != nil {
		util.LogError("failed to write ws-replay.js", err)
	} else {
		util.LogInfo("wrote ws-replay.js", zap.String("path", wsReplayPath))
	}

	wsDataPath := c.cfg.OutputDir + "/ws-data.json"
	if wsData, err := json.MarshalIndent(wsByURL, "", "  "); err == nil {
		if err := os.WriteFile(wsDataPath, wsData, 0644); err != nil {
			util.LogError("failed to write ws-data.json", err)
		} else {
			util.LogInfo("wrote ws-data.json", zap.String("path", wsDataPath), zap.Int("ws_urls", len(wsByURL)))
		}
	}
}

func (c *Crawler) writeSitemap() {
	c.routeMu.RLock()
	urls := make([]string, 0, len(c.discoveredRoutes))
	for u := range c.discoveredRoutes {
		urls = append(urls, u)
	}
	c.routeMu.RUnlock()
	if len(urls) == 0 {
		return
	}
	sort.Strings(urls)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range urls {
		escaped := strings.ReplaceAll(u, "&", "&amp;")
		escaped = strings.ReplaceAll(escaped, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		b.WriteString(fmt.Sprintf("  <url><loc>%s</loc></url>\n", escaped))
	}
	b.WriteString("</urlset>\n")
	path := c.cfg.OutputDir + "/sitemap.xml"
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		util.LogError("failed to write sitemap.xml", err)
		return
	}
	util.LogInfo("wrote sitemap", zap.String("path", path), zap.Int("urls", len(urls)))
}

func extractGraphQLOp(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var gqlReq struct {
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(body, &gqlReq); err != nil {
		return ""
	}
	return gqlReq.OperationName
}

func jsonHeadrs(h map[string]string) string {
	if len(h) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range h {
		if !first {
			b.WriteByte(',')
		}
		first = false
		kk, _ := json.Marshal(k)
		vv, _ := json.Marshal(v)
		b.WriteString(fmt.Sprintf("%s:%s", string(kk), string(vv)))
	}
	b.WriteByte('}')
	return b.String()
}

func (c *Crawler) saveCheckpoint() {
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
	c.checkpoint.Save(c.urlQueue, hlc, huc)
}

func (c *Crawler) launchBrowser() error {
	c.browserMu.Lock()
	defer c.browserMu.Unlock()

	if c.browserCancel != nil {
		c.browserCancel()
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(c.ctx, c.allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		allocCancel()
		browserCancel()
		return fmt.Errorf("browser launch failed: %w", err)
	}

	go func() {
		<-allocCtx.Done()
		browserCancel()
	}()

	c.browserCtx = browserCtx
	c.browserCancel = func() {
		allocCancel()
		browserCancel()
	}
	return nil
}

func (c *Crawler) getBrowserCtx() context.Context {
	c.browserMu.Lock()
	defer c.browserMu.Unlock()
	return c.browserCtx
}

func (c *Crawler) restartBrowser() context.Context {
	c.browserMu.Lock()
	if c.browserCancel != nil {
		c.browserCancel()
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(c.ctx, c.allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		allocCancel()
		browserCancel()
		c.browserMu.Unlock()
		util.LogError("browser restart failed", err)
		return nil
	}

	go func() {
		<-allocCtx.Done()
		browserCancel()
	}()

	c.browserCtx = browserCtx
	c.browserCancel = func() {
		allocCancel()
		browserCancel()
	}
	c.browserMu.Unlock()
	util.LogInfo("browser restarted successfully")
	return browserCtx
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
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			case <-c.ctx.Done():
				return
			}
		}

		browserCtx := c.getBrowserCtx()
		if browserCtx == nil {
			util.LogError("browser context is nil, skipping", nil, zap.String("url", urlStr))
			return
		}

		if browserCtx.Err() != nil {
			util.LogInfo("browser appears to have crashed, restarting",
				zap.String("url", urlStr),
				zap.Error(browserCtx.Err()),
			)
			browserCtx = c.restartBrowser()
			if browserCtx == nil {
				util.LogError("browser restart failed, giving up", nil, zap.String("url", urlStr))
				return
			}
		}

		lastErr = c.doCrawl(browserCtx, urlStr, depth)
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

	if c.authManager != nil && c.cfg.AuthConfig != nil && c.cfg.AuthConfig.Enabled {
		if err := c.authManager.Authenticate(tabCtx, urlStr); err != nil {
			util.LogError("authentication failed", err, zap.String("url", urlStr))
		}
	}

	workerCount := c.cfg.MaxConcurrentPages * 2
	if workerCount < 5 {
		workerCount = 5
	}
	netIntercept := netintercept.NewInterceptorWithWorkers(workerCount)
	netIntercept.SetAPICallback(func(ar netintercept.APIResponse) {
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
		if c.cfg.EnableWARC && c.warc != nil {
			c.warc.WriteRecord(&storage.WARCRecord{
				URL:        ar.URL,
				Body:       ar.Body,
				MimeType:   "application/json",
				Date:       time.Now(),
				StatusCode: ar.StatusCode,
				RecordType: "response",
				ContentLen: int64(len(ar.Body)),
			})
		}
		savePath := c.storage.PathForAPI(ar.URL)
		if savePath != "" {
			os.MkdirAll(filepath.Dir(savePath), 0755)
			os.WriteFile(savePath, ar.Body, 0644)
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
			return fmt.Errorf("navigation error: %w", fmt.Errorf("%s", errorText))
		}
		return nil
	}))
	if err != nil {
		errStr := err.Error()
		retryable := true
		for _, code := range []string{"404", "410", "403", "401", "400"} {
			if strings.Contains(errStr, code) {
				retryable = false
				break
			}
		}
		return &RetryableError{Err: err, Retryable: retryable}
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
		select {
		case <-time.After(500 * time.Millisecond):
		case <-c.ctx.Done():
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
			for _, routeURL := range routesToCrawl {
				select {
				case <-c.ctx.Done():
					break
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
				os.WriteFile(savePath, structuredData, 0644)
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
			os.WriteFile(articlePath, articleData, 0644)
		}
	}

	if c.cfg.EnableSingleFile {
		singleFile, err := jsengine.GenerateSingleFile(tabCtx)
		if err == nil && singleFile != "" {
			singleFilePath := c.cfg.OutputDir + "/" + getHost(urlStr) + "/singlefile.html"
			os.MkdirAll(filepath.Dir(singleFilePath), 0755)
			os.WriteFile(singleFilePath, []byte(singleFile), 0644)
		}
	}

	var html string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(`
		(function() {
			function serializeShadowDOM(root) {
				var elements = root.querySelectorAll('*');
				elements.forEach(function(el) {
					if (el.shadowRoot) {
						var template = document.createElement('template');
						template.innerHTML = '<!---- shadowrootmode=open ---->' + el.shadowRoot.innerHTML + '<!---- /shadowrootmode ---->';
						el.appendChild(template.content);
					}
				});
			}
			serializeShadowDOM(document);
			var doc = document.documentElement;
			if (!doc) return document.body ? document.body.outerHTML : '';
			return '<!DOCTYPE html>' + doc.outerHTML;
		})()
	`, &html)); err != nil || html == "" {
		chromedp.Run(tabCtx, chromedp.Evaluate(`
			(function() {
				function serializeShadowDOM(root) {
					root.querySelectorAll('*').forEach(function(el) {
						if (el.shadowRoot) {
							var t = document.createElement('template');
							t.innerHTML = el.shadowRoot.innerHTML;
							el.appendChild(t.content);
						}
					});
				}
				serializeShadowDOM(document);
				return document.documentElement ? '<!DOCTYPE html>' + document.documentElement.outerHTML : '';
			})()
		`, &html))
		if html == "" {
			chromedp.Run(tabCtx, chromedp.OuterHTML("html", &html))
			if html != "" {
				html = "<!DOCTYPE html>\n" + html
			}
		}
	}
	if html == "" {
		chromedp.Run(tabCtx, chromedp.Evaluate(`document.body ? document.body.innerHTML : ''`, &html))
	}

	if c.changeDetector != nil {
		var title string
		chromedp.Run(tabCtx, chromedp.Title(&title))
		newSnap, err := c.changeDetector.SaveSnapshot(urlStr, title, []byte(html))
		if err != nil {
			util.LogDebug("failed to save snapshot", zap.Error(err))
		} else {
			oldSnap, _ := c.changeDetector.LoadSnapshot(urlStr)
			if oldSnap != nil && oldSnap.Hash != newSnap.Hash {
				report := c.changeDetector.DetectChanges(urlStr, oldSnap, newSnap)
				util.LogInfo("page changed",
					zap.String("url", urlStr),
					zap.Int("changes", len(report.Changes)),
					zap.String("old_hash", report.OldHash),
					zap.String("new_hash", report.NewHash),
				)
			}
		}
	}

	if c.captchaSolver != nil {
		c.solveCaptcha(tabCtx, urlStr, html)
	}

	c.metrics.IncPagesFetched()

	framework, _ := jsengine.DetectFramework(tabCtx)
	if framework != nil {
		util.LogDebug("framework",
			zap.String("url", urlStr),
			zap.String("name", framework.Framework),
		)
	}

	var screenshot []byte
	if c.cfg.EnableScreenshot {
		if err := chromedp.Run(tabCtx, chromedp.FullScreenshot(&screenshot, 80)); err != nil {
			util.LogDebug("screenshot failed", zap.Error(err))
		}
	}

	var pdfData []byte
	if c.cfg.EnablePDF {
		if err := chromedp.Run(tabCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				pdfParams := page.PrintToPDF()
				pdfParams.PrintBackground = true
				var err error
				pdfData, _, err = pdfParams.Do(ctx)
				return err
			}),
		); err != nil {
			util.LogDebug("pdf failed", zap.Error(err))
		}
	}

	htmlLocalPath, err := c.storage.SaveHTML(urlStr, []byte(html))
	if err != nil {
		return err
	}

	c.rewriter.SetBaseURL(urlStr)

	if c.cfg.EnableWARC && c.warc != nil {
		c.warc.WriteRecord(&storage.WARCRecord{
			URL:        urlStr,
			Body:       []byte(html),
			MimeType:   "text/html",
			Date:       time.Now(),
			StatusCode: 200,
			RecordType: "response",
			ContentLen: int64(len(html)),
		})
	}

	if screenshot != nil {
		c.storage.SaveScreenshot(urlStr, screenshot)
	}
	if pdfData != nil {
		c.storage.SavePDF(urlStr, pdfData)
	}

	netIntercept.FetchBodies(rawTabCtx)

	// First pass: save all CDP-captured resources
	cdpSaved := make(map[string]bool)
	for origURL, resource := range netIntercept.GetResources() {
		if c.cfg.Incremental && resource.StatusCode == 304 {
			continue
		}

		hashStr := strconv.FormatUint(xxhash.Sum64(resource.Body), 36)

		dup := c.contentHashes.Contains(hashStr)
		if !dup {
			c.contentHashes.Add(hashStr)
		}

		if dup {
			if c.cfg.Incremental && c.incCache != nil {
				c.incCache.UpdateFromResponse(origURL, int(resource.StatusCode), resource.Headers)
			}
			continue
		}

		localPath, err := c.storage.SaveFile(origURL, resource.Body, resource.MimeType)
		if err != nil {
			continue
		}
		c.rewriter.AddMapping(origURL, localPath)

		relPath, err := filepath.Rel(filepath.Dir(htmlLocalPath), localPath)
		if err != nil {
			relPath = localPath
		}
		relPath = filepath.ToSlash(relPath)
		c.rewriter.AddAbsoluteToRelMapping(origURL, relPath)

		c.metrics.IncAssetsCaptured()
		c.metrics.AddBytes(int64(len(resource.Body)))

		if c.cfg.EnableWARC && c.warc != nil {
			c.warc.WriteRecord(&storage.WARCRecord{
				URL:        origURL,
				Body:       resource.Body,
				MimeType:   resource.MimeType,
				Date:       time.Now(),
				StatusCode: 200,
				RecordType: "response",
				ContentLen: int64(len(resource.Body)),
			})
		}

		if c.cfg.Incremental && c.incCache != nil {
			c.incCache.UpdateFromResponse(origURL, int(resource.StatusCode), resource.Headers)
		}
		cdpSaved[origURL] = true
	}

	// HTTP fallback: download CDP-seen resources where body fetch failed
	for _, missingURL := range netIntercept.GetMissingResources() {
		if !isValidURL(missingURL) || !c.isAllowedDomain(missingURL) || c.isExcluded(missingURL) {
			continue
		}
		if cdpSaved[missingURL] {
			continue
		}
		resource, err := netIntercept.DownloadResourceViaHTTP(missingURL)
		if err != nil || resource == nil || len(resource.Body) == 0 {
			continue
		}
		hashStr := strconv.FormatUint(xxhash.Sum64(resource.Body), 36)
		if c.contentHashes.Contains(hashStr) {
			continue
		}
		c.contentHashes.Add(hashStr)
		localPath, err := c.storage.SaveFile(missingURL, resource.Body, resource.MimeType)
		if err != nil {
			continue
		}
		c.rewriter.AddMapping(missingURL, localPath)
		relPath, _ := filepath.Rel(filepath.Dir(htmlLocalPath), localPath)
		c.rewriter.AddAbsoluteToRelMapping(missingURL, filepath.ToSlash(relPath))
		c.metrics.IncAssetsCaptured()
		c.metrics.AddBytes(int64(len(resource.Body)))
	}

	// Download assets from HTML not seen by CDP at all
	if html != "" {
		c.downloadHTMLAssets(urlStr, html, htmlLocalPath, netIntercept, cdpSaved)
	}

	for cssPath := range c.rewriter.GetCSSFiles() {
		c.rewriter.ProcessFiles(map[string]string{cssPath: "css"})

		// Extract and download fonts referenced in CSS
		cssData, err := os.ReadFile(cssPath)
		if err == nil {
			fontURLs := c.rewriter.ExtractFontURLs(cssData)
			for _, fontURL := range fontURLs {
				absFontURL := rewrite.ResolveURL(urlStr, fontURL)
				if absFontURL != "" && isValidURL(absFontURL) {
					if !c.bloomFilter.HasSeen(absFontURL) && !c.exactDedup.Contains(absFontURL) {
						fontResp, err := c.httpClient.Get(absFontURL)
						if err == nil && fontResp.StatusCode == 200 {
							fontBody, _ := io.ReadAll(fontResp.Body)
							fontResp.Body.Close()
							if len(fontBody) > 0 {
								localPath, err := c.storage.SaveFile(absFontURL, fontBody, fontResp.Header.Get("Content-Type"))
								if err == nil {
									c.rewriter.AddMapping(absFontURL, localPath)
									c.metrics.IncAssetsCaptured()
									c.metrics.AddBytes(int64(len(fontBody)))
								}
							}
						}
					}
				}
			}

			// Extract and download ALL CSS url() references (background images, etc.)
			cssURLs := c.rewriter.ExtractAllCSSURLs(cssData)
			for _, cssURL := range cssURLs {
				absCSSURL := rewrite.ResolveURL(urlStr, cssURL)
				if absCSSURL != "" && isValidURL(absCSSURL) {
					if !c.bloomFilter.HasSeen(absCSSURL) && !c.exactDedup.Contains(absCSSURL) {
						cssResp, err := c.httpClient.Get(absCSSURL)
						if err == nil && cssResp.StatusCode == 200 {
							cssBody, _ := io.ReadAll(cssResp.Body)
							cssResp.Body.Close()
							if len(cssBody) > 0 {
								localPath, err := c.storage.SaveFile(absCSSURL, cssBody, cssResp.Header.Get("Content-Type"))
								if err == nil {
									c.rewriter.AddMapping(absCSSURL, localPath)
									c.metrics.IncAssetsCaptured()
									c.metrics.AddBytes(int64(len(cssBody)))
								}
							}
						}
					}
				}
			}
		}
	}
	c.resolveJSDependencies(htmlLocalPath, urlStr)
	c.rewriter.ProcessFiles(map[string]string{htmlLocalPath: "html"})

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
		if !isValidURL(normalized) {
			continue
		}
		if c.bloomFilter.HasSeen(normalized) {
			continue
		}
		if !c.isAllowedDomain(normalized) {
			continue
		}
		if c.isExcluded(normalized) {
			continue
		}
		if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
			break
		}
		if c.urlQueue.Size() >= maxQueueSize {
			util.LogDebug("queue full, skipping", zap.String("url", normalized))
			continue
		}
		if c.exactDedup.Contains(normalized) {
			continue
		}
		c.bloomFilter.Add(normalized)
		c.exactDedup.Add(normalized)
		c.urlQueue.PushURL(normalized, depth+1)
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
				if absURL == "" || !isValidURL(absURL) {
					continue
				}
				normalized := c.normalizeURL(queue.NormalizeAndClean(absURL))
				if c.bloomFilter.HasSeen(normalized) {
					continue
				}
				if !c.isAllowedDomain(normalized) {
					continue
				}
				if c.isExcluded(normalized) {
					continue
				}
				if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
					break
				}
				if c.urlQueue.Size() >= maxQueueSize {
					continue
				}
				if c.exactDedup.Contains(normalized) {
					continue
				}
				c.bloomFilter.Add(normalized)
				c.exactDedup.Add(normalized)
				c.urlQueue.PushURL(normalized, depth+1)
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
				if absURL == "" || !isValidURL(absURL) {
					continue
				}
				normalized := c.normalizeURL(queue.NormalizeAndClean(absURL))
				if c.bloomFilter.HasSeen(normalized) {
					continue
				}
				if !c.isAllowedDomain(normalized) {
					continue
				}
				if c.isExcluded(normalized) {
					continue
				}
				if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
					break
				}
				if c.urlQueue.Size() >= maxQueueSize {
					continue
				}
				if c.exactDedup.Contains(normalized) {
					continue
				}
				c.bloomFilter.Add(normalized)
				c.exactDedup.Add(normalized)
				c.urlQueue.PushURL(normalized, depth+1)

				if ms.Poster != "" {
					posterURL := rewrite.ResolveURL(urlStr, ms.Poster)
					if posterURL != "" && isValidURL(posterURL) {
						pNorm := c.normalizeURL(queue.NormalizeAndClean(posterURL))
						if !c.bloomFilter.HasSeen(pNorm) && c.isAllowedDomain(pNorm) && !c.isExcluded(pNorm) && !c.exactDedup.Contains(pNorm) {
							c.bloomFilter.Add(pNorm)
							c.exactDedup.Add(pNorm)
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

	return nil
}

func (c *Crawler) downloadHTMLAssets(baseURL, pageHTML, htmlLocalPath string, netIntercept *netintercept.Interceptor, cdpSaved map[string]bool) {
	c.rewriter.SetBaseURL(baseURL)

	assetURLs := make(map[string]bool)
	tokenizer := html.NewTokenizer(strings.NewReader(pageHTML))
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		_, hasAttr := tokenizer.TagName()

		var attrs []struct{ k, v string }
		if hasAttr {
			for {
				k, v, more := tokenizer.TagAttr()
				attrs = append(attrs, struct{ k, v string }{string(k), string(v)})
				if !more {
					break
				}
			}
		}

		for _, attr := range attrs {
			ak := attr.k
			av := attr.v
			if av == "" || av == "#" || strings.HasPrefix(av, "javascript:") || strings.HasPrefix(av, "mailto:") || strings.HasPrefix(av, "data:") {
				continue
			}
			var resourceURL string
			switch ak {
			case "src", "href", "action", "poster", "data":
				resourceURL = av
			case "srcset":
				parts := strings.Split(av, ",")
				for _, part := range parts {
					urlPart := strings.TrimSpace(strings.Split(strings.TrimSpace(part), " ")[0])
					if urlPart != "" {
						absURL := rewrite.ResolveURL(baseURL, urlPart)
						if absURL != "" && isValidURL(absURL) {
							assetURLs[absURL] = true
						}
					}
				}
				continue
			default:
				if strings.HasPrefix(ak, "data-") && (strings.Contains(ak, "src") || strings.Contains(ak, "url") || strings.Contains(ak, "bg") || strings.Contains(ak, "image") || strings.Contains(ak, "lazy")) {
					resourceURL = av
				}
			}
			if resourceURL == "" {
				continue
			}
			absURL := rewrite.ResolveURL(baseURL, resourceURL)
			if absURL != "" && isValidURL(absURL) {
				assetURLs[absURL] = true
			}
		}
	}

	var g errgroup.Group
	g.SetLimit(5)
	for assetURL := range assetURLs {
		if cdpSaved[assetURL] {
			continue
		}
		if c.bloomFilter.HasSeen(assetURL) || c.exactDedup.Contains(assetURL) {
			continue
		}
		if !c.isAllowedDomain(assetURL) || c.isExcluded(assetURL) {
			continue
		}

		u := assetURL
		g.Go(func() error {
			resource, err := netIntercept.DownloadResourceViaHTTP(u)
			if err != nil || resource == nil || len(resource.Body) == 0 {
				return nil
			}

			hashStr := strconv.FormatUint(xxhash.Sum64(resource.Body), 36)
			if c.contentHashes.Contains(hashStr) {
				return nil
			}
			c.contentHashes.Add(hashStr)

			localPath, err := c.storage.SaveFile(u, resource.Body, resource.MimeType)
			if err != nil {
				return nil
			}
			c.rewriter.AddMapping(u, localPath)
			relPath, _ := filepath.Rel(filepath.Dir(htmlLocalPath), localPath)
			c.rewriter.AddAbsoluteToRelMapping(u, filepath.ToSlash(relPath))
			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(resource.Body)))
			return nil
		})
	}
	g.Wait()
}

func (c *Crawler) resolveJSDependencies(htmlLocalPath, baseURL string) {
	htmlDir := filepath.Dir(htmlLocalPath)
	jsFiles := make(map[string]string)

	for filePath := range c.rewriter.GetMappings() {
		if strings.HasSuffix(filePath, ".js") {
			localPath := c.rewriter.GetMappings()[filePath]
			if localPath != "" {
				jsFiles[filePath] = localPath
			}
		}
	}

	for _, localPath := range jsFiles {
		jsData, err := os.ReadFile(localPath)
		if err != nil {
			continue
		}

		analyzedURLs := jsanalyzer.ExtractJSURLs(string(jsData), baseURL)

		for _, au := range analyzedURLs {
			if c.bloomFilter.HasSeen(au.URL) || c.exactDedup.Contains(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}

			resp, err := c.httpClient.Get(au.URL)
			if err != nil || resp.StatusCode != 200 {
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(body) == 0 {
				continue
			}

			hashStr := strconv.FormatUint(xxhash.Sum64(body), 36)
			if c.contentHashes.Contains(hashStr) {
				continue
			}
			c.contentHashes.Add(hashStr)

			savedPath, err := c.storage.SaveFile(au.URL, body, resp.Header.Get("Content-Type"))
			if err != nil {
				continue
			}
			c.rewriter.AddMapping(au.URL, savedPath)

			relPath, _ := filepath.Rel(htmlDir, savedPath)
			relPath = filepath.ToSlash(relPath)
			c.rewriter.AddAbsoluteToRelMapping(au.URL, relPath)

			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(body)))

			if strings.HasSuffix(au.URL, ".js") {
				c.resolveJSDependenciesRecursive(au.URL, baseURL, htmlDir, 0)
			}
		}

		htmlURLs := jsanalyzer.ExtractFromHTML(string(jsData), baseURL)
		for _, au := range htmlURLs {
			if c.bloomFilter.HasSeen(au.URL) || c.exactDedup.Contains(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}
			resp, err := c.httpClient.Get(au.URL)
			if err != nil || resp.StatusCode != 200 {
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(body) == 0 {
				continue
			}

			savedPath, err := c.storage.SaveFile(au.URL, body, resp.Header.Get("Content-Type"))
			if err != nil {
				continue
			}
			c.rewriter.AddMapping(au.URL, savedPath)
			relPath, _ := filepath.Rel(htmlDir, savedPath)
			relPath = filepath.ToSlash(relPath)
			c.rewriter.AddAbsoluteToRelMapping(au.URL, relPath)

			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(body)))
		}
	}
}

func (c *Crawler) resolveJSDependenciesRecursive(jsURL, baseURL, htmlDir string, depth int) {
	if depth > 3 {
		return
	}

	if c.bloomFilter.HasSeen(jsURL) || c.exactDedup.Contains(jsURL) {
		return
	}
	if !c.isAllowedDomain(jsURL) || c.isExcluded(jsURL) {
		return
	}

	resp, err := c.httpClient.Get(jsURL)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		return
	}

	hashStr := strconv.FormatUint(xxhash.Sum64(body), 36)
	if c.contentHashes.Contains(hashStr) {
		return
	}
	c.contentHashes.Add(hashStr)

	savedPath, err := c.storage.SaveFile(jsURL, body, resp.Header.Get("Content-Type"))
	if err != nil {
		return
	}
	c.rewriter.AddMapping(jsURL, savedPath)

	relPath, _ := filepath.Rel(htmlDir, savedPath)
	relPath = filepath.ToSlash(relPath)
	c.rewriter.AddAbsoluteToRelMapping(jsURL, relPath)

	c.metrics.IncAssetsCaptured()
	c.metrics.AddBytes(int64(len(body)))

	analyzedURLs := jsanalyzer.ExtractJSURLs(string(body), baseURL)
	for _, au := range analyzedURLs {
		if strings.HasSuffix(au.URL, ".js") {
			c.resolveJSDependenciesRecursive(au.URL, baseURL, htmlDir, depth+1)
		} else {
			if c.bloomFilter.HasSeen(au.URL) || c.exactDedup.Contains(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}
			resp2, err := c.httpClient.Get(au.URL)
			if err != nil || resp2.StatusCode != 200 {
				if resp2 != nil {
					resp2.Body.Close()
				}
				continue
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			if len(body2) == 0 {
				continue
			}

			hashStr2 := strconv.FormatUint(xxhash.Sum64(body2), 36)
			if c.contentHashes.Contains(hashStr2) {
				continue
			}
			c.contentHashes.Add(hashStr2)

			savedPath2, err := c.storage.SaveFile(au.URL, body2, resp2.Header.Get("Content-Type"))
			if err != nil {
				continue
			}
			c.rewriter.AddMapping(au.URL, savedPath2)
			relPath2, _ := filepath.Rel(htmlDir, savedPath2)
			relPath2 = filepath.ToSlash(relPath2)
			c.rewriter.AddAbsoluteToRelMapping(au.URL, relPath2)

			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(body2)))
		}
	}
}

func (c *Crawler) waitForPage(ctx context.Context) {
	waitCtx, cancel := context.WithTimeout(ctx, c.cfg.WaitForPageTimeout)
	defer cancel()

	if err := chromedp.Run(waitCtx, chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		util.LogDebug("wait ready failed", zap.Error(err))
	}

	netCtx, netCancel := context.WithTimeout(waitCtx, c.cfg.WaitForPageTimeout)
	defer netCancel()
	jsengine.WaitForNetworkIdle(netCtx, c.cfg.NetworkIdleQuiet)
}

func (c *Crawler) setCookies(ctx context.Context) {
	if len(c.cfg.Cookies) == 0 {
		return
	}
	for _, cookie := range c.cfg.Cookies {
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			params := network.SetCookie(cookie.Name, cookie.Value)
			if cookie.Domain != "" {
				params = params.WithDomain(cookie.Domain)
			}
			if cookie.Path != "" {
				params = params.WithPath(cookie.Path)
			}
			if cookie.HTTPOnly {
				params = params.WithHTTPOnly(true)
			}
			return params.Do(ctx)
		})); err != nil {
			util.LogDebug("set cookie failed", zap.Error(err))
		}
	}
}

func (c *Crawler) runInteractionEngine(ctx context.Context, urlStr string) {
	if !c.cfg.EnableInteractionEngine {
		return
	}

	maxInteractions := c.cfg.MaxInteractionsPerPage
	if maxInteractions <= 0 {
		maxInteractions = 50
	}

	interactionCount := 0

	interactedElements := make(map[string]bool)

	for interactionCount < maxInteractions {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		script := `
			(function() {
				var results = [];
				var selectors = [
					'button:not([disabled]):not([type="submit"]):not([type="reset"])',
					'a[href]:not([href^="mailto:"]):not([href^="tel:"]):not([href^="javascript:"]):not([target="_blank"])',
					'input[type="button"]:not([disabled])',
					'input[type="submit"]:not([disabled])',
					'[role="button"]:not([aria-disabled="true"])',
					'[onclick]',
					'.btn:not([disabled])',
					'button.btn:not([disabled])',
					'[data-action]',
					'[data-click]',
					'[data-toggle]',
					'[data-dismiss]',
					'summary',
					'details:not([open]) > summary',
					'.accordion-header',
					'.accordion-trigger',
					'[aria-expanded="false"]',
					'.collapsible:not(.active)',
					'[data-bs-toggle="collapse"]',
					'[data-bs-toggle="modal"]',
					'[data-toggle="tab"]',
					'[data-toggle="pill"]'
				];

				for (var i = 0; i < selectors.length; i++) {
					var elements = document.querySelectorAll(selectors[i]);
					for (var j = 0; j < elements.length; j++) {
						var el = elements[j];
						if (!el.offsetParent && el.tagName !== 'SUMMARY') continue;
						var rect = el.getBoundingClientRect();
						if (rect.width === 0 && rect.height === 0) continue;
						var xpath = getXPath(el);
						if (xpath) {
							results.push({
								xpath: xpath,
								tag: el.tagName.toLowerCase(),
								text: (el.textContent || '').trim().substring(0, 100),
								href: el.href || '',
								type: 'click'
							});
						}
					}
				}

				var forms = document.querySelectorAll('form');
				for (var i = 0; i < forms.length; i++) {
					var form = forms[i];
					var action = form.action || '';
					var method = (form.method || 'GET').toUpperCase();
					if (action && action.indexOf('javascript:') === -1) {
						var xpath = getXPath(form);
						if (xpath) {
							results.push({
								xpath: xpath,
								tag: 'form',
								action: action,
								method: method,
								type: 'form'
							});
						}
					}
				}

				var inputs = document.querySelectorAll('input[type="text"], input[type="email"], input[type="password"], input[type="search"], textarea, select');
				for (var i = 0; i < inputs.length; i++) {
					var input = inputs[i];
					if (!input.offsetParent) continue;
					var xpath = getXPath(input);
					if (xpath) {
						results.push({
							xpath: xpath,
							tag: input.tagName.toLowerCase(),
							type: input.type || '',
							name: input.name || '',
							placeholder: input.placeholder || '',
							type: 'fill'
						});
					}
				}

				return results;

				function getXPath(element) {
					if (element.id !== '') {
						return 'id("' + element.id + '")';
					}
					if (element === document.body) {
						return '/html/body';
					}
					var ix = 0;
					var siblings = element.parentNode.childNodes;
					for (var i = 0; i < siblings.length; i++) {
						var sibling = siblings[i];
						if (sibling === element) {
							return getXPath(element.parentNode) + '/' + element.tagName.toLowerCase() + '[' + (ix + 1) + ']';
						}
						if (sibling.nodeType === 1 && sibling.tagName === element.tagName) {
							ix++;
						}
					}
					return null;
				}
			})()
		`

		var result []map[string]interface{}
		err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
		if err != nil {
			util.LogDebug("interaction discovery failed", zap.Error(err))
			break
		}

		if len(result) == 0 {
			break
		}

		foundNew := false
		for _, item := range result {
			if interactionCount >= maxInteractions {
				break
			}

			xpath := ""
			if x, ok := item["xpath"].(string); ok {
				xpath = x
			}

			if xpath == "" || interactedElements[xpath] {
				continue
			}

			itemType := ""
			if t, ok := item["type"].(string); ok {
				itemType = t
			}

			handled := false

			switch itemType {
			case "click":
				handled = c.clickElement(ctx, xpath, item)
			case "form":
				handled = c.interactWithForm(ctx, xpath, item)
			case "fill":
				handled = c.fillInput(ctx, xpath, item)
			}

			if handled {
				interactedElements[xpath] = true
				interactionCount++
				foundNew = true

				waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				jsengine.WaitForNetworkIdle(waitCtx, 1*time.Second)
				cancel()

				time.Sleep(500 * time.Millisecond)
			}
		}

		if !foundNew {
			break
		}
	}

	if c.cfg.EnableLazyLoad {
		jsengine.InjectLazyLoad(ctx)
	}

	scrollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	jsengine.InfiniteScroll(scrollCtx, &jsengine.InfiniteScrollConfig{
		Enabled:          true,
		MaxScrolls:       10,
		MaxDuration:      10 * time.Second,
		StablePasses:     2,
		ItemSelector:     "article, .card, .list-item, [data-infinite-scroll-item], .feed-item",
		ScrollContainer:  "",
		LoadMoreSelector: "",
		ScrollDelay:      1 * time.Second,
		ScrollDistance:   500,
	})
	cancel()
}

func (c *Crawler) clickElement(ctx context.Context, xpath string, item map[string]interface{}) bool {
	script := fmt.Sprintf(`
		(function(xpath) {
			var result = document.evaluate(xpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
			var el = result.singleNodeValue;
			if (!el) return { success: false, reason: 'not found' };
			if (el.tagName === 'A' && el.href) {
				var url = el.href;
				if (url.startsWith('http') && !url.startsWith(window.location.origin)) {
					return { success: false, reason: 'external link' };
				}
			}
			try {
				el.click();
				return { success: true, tag: el.tagName };
			} catch(e) {
				return { success: false, reason: e.message };
			}
		})("%s")
	`, xpath)

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false
	}

	if success, ok := result["success"].(bool); ok && success {
		tag := ""
		if t, ok := result["tag"].(string); ok {
			tag = t
		}
		util.LogDebug("interaction: clicked element", zap.String("xpath", xpath), zap.String("tag", tag))
		return true
	}
	return false
}

func (c *Crawler) interactWithForm(ctx context.Context, xpath string, item map[string]interface{}) bool {
	action := ""
	if a, ok := item["action"].(string); ok {
		action = a
	}
	method := ""
	if m, ok := item["method"].(string); ok {
		method = m
	}

	util.LogDebug("interaction: found form", zap.String("xpath", xpath), zap.String("action", action), zap.String("method", method))
	return false
}

func (c *Crawler) fillInput(ctx context.Context, xpath string, item map[string]interface{}) bool {
	inputType := ""
	if t, ok := item["type"].(string); ok {
		inputType = t
	}
	name := ""
	if n, ok := item["name"].(string); ok {
		name = n
	}
	placeholder := ""
	if p, ok := item["placeholder"].(string); ok {
		placeholder = p
	}

	value := "test"
	if inputType == "email" {
		value = "test@example.com"
	} else if inputType == "password" {
		value = "testpassword123"
	} else if strings.Contains(strings.ToLower(name), "search") || strings.Contains(strings.ToLower(placeholder), "search") {
		value = "test query"
	}

	script := fmt.Sprintf(`
		(function(xpath, value) {
			var result = document.evaluate(xpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
			var el = result.singleNodeValue;
			if (!el) return { success: false, reason: 'not found' };
			try {
				el.focus();
				el.value = value;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return { success: true };
			} catch(e) {
				return { success: false, reason: e.message };
			}
		})("%s", "%s")
	`, xpath, value)

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false
	}

	if success, ok := result["success"].(bool); ok && success {
		util.LogDebug("interaction: filled input", zap.String("xpath", xpath), zap.String("name", name))
		return true
	}
	return false
}

const cookieJarFile = "cookies.json"

func (c *Crawler) saveCookieJar() {
	c.cookieMu.RLock()
	defer c.cookieMu.RUnlock()
	if len(c.cookieJar) == 0 {
		return
	}
	data, err := json.MarshalIndent(c.cookieJar, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(c.cfg.OutputDir, cookieJarFile)
	os.MkdirAll(c.cfg.OutputDir, 0755)
	os.WriteFile(path, data, 0644)
}

func (c *Crawler) loadCookieJar() {
	path := filepath.Join(c.cfg.OutputDir, cookieJarFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var jar map[string][]*http.Cookie
	if err := json.Unmarshal(data, &jar); err != nil {
		return
	}
	c.cookieMu.Lock()
	for domain, cookies := range jar {
		c.cookieJar[domain] = cookies
	}
	c.cookieMu.Unlock()
	util.LogInfo("loaded cookie jar", zap.Int("domains", len(jar)))
}

func (c *Crawler) persistCookies(ctx context.Context, urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	domain := parsed.Hostname()
	if domain == "" {
		return
	}

	var cookies []*http.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cctx context.Context) error {
		cdpCookies, err := network.GetCookies().Do(cctx)
		if err != nil {
			return err
		}
		cookies = util.CDPCookiesToHTTP(cdpCookies)
		return nil
	})); err != nil {
		util.LogDebug("failed to persist cookies", zap.Error(err))
		return
	}

	c.cookieMu.Lock()
	for _, ck := range cookies {
		ckDomain := domain
		if ck.Domain != "" {
			ckDomain = strings.TrimPrefix(ck.Domain, ".")
		}
		c.cookieJar[ckDomain] = append(c.cookieJar[ckDomain], ck)
	}
	c.cookieMu.Unlock()

	util.LogDebug("persisted cookies", zap.String("domain", domain), zap.Int("count", len(cookies)))
}

func (c *Crawler) injectPersistedCookies(ctx context.Context, urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	domain := parsed.Hostname()
	if domain == "" {
		return
	}

	c.cookieMu.RLock()
	var allCookies []*http.Cookie
	// Collect cookies from the exact domain and parent domains
	parts := strings.Split(domain, ".")
	seen := make(map[string]bool)
	for i := 0; i < len(parts); i++ {
		d := strings.Join(parts[i:], ".")
		for _, ck := range c.cookieJar[d] {
			key := ck.Name + "|" + ck.Domain + "|" + ck.Path
			if !seen[key] {
				seen[key] = true
				allCookies = append(allCookies, ck)
			}
		}
	}
	c.cookieMu.RUnlock()

	for _, cookie := range allCookies {
		cp := network.SetCookie(cookie.Name, cookie.Value)
		if cookie.Domain != "" {
			cp = cp.WithDomain(cookie.Domain)
		}
		if cookie.Path != "" {
			cp = cp.WithPath(cookie.Path)
		}
		if cookie.Secure {
			cp = cp.WithSecure(true)
		}
		if cookie.HttpOnly {
			cp = cp.WithHTTPOnly(true)
		}
		if !cookie.Expires.IsZero() {
			expires := cdp.TimeSinceEpoch(cookie.Expires)
			cp = cp.WithExpires(&expires)
		}
		if err := chromedp.Run(ctx, cp); err != nil {
			util.LogDebug("failed to inject persisted cookie", zap.Error(err))
		}
	}

	if len(allCookies) > 0 {
		util.LogDebug("injected persisted cookies",
			zap.String("domain", domain),
			zap.Int("count", len(allCookies)),
		)
	}
}

func (c *Crawler) setupConsoleCapture(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			level := string(e.Type)
			if level != "error" && level != "warning" {
				return
			}
			var msg string
			for _, arg := range e.Args {
				if arg.Value != nil {
					msg += string(arg.Value) + " "
				}
			}
			c.jsErrors.Push(JSError{
				Message: strings.TrimSpace(msg),
				Level:   level,
			})
		case *cdpruntime.EventExceptionThrown:
			c.jsErrors.Push(JSError{
				Message: e.ExceptionDetails.Error(),
				Level:   "exception",
			})
		}
	})
}

func (c *Crawler) setupWSCapture(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventWebSocketCreated:
			c.wsMu.Lock()
			c.wsURLs[e.RequestID] = e.URL
			c.wsMu.Unlock()
		case *network.EventWebSocketFrameSent:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			if wsURL == "" {
				return
			}
			isBinary := e.Response.Opcode == 2
			data := e.Response.PayloadData
			if isBinary {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "send",
				Data:      data,
				Timestamp: time.Now(),
				Opcode:    e.Response.Opcode,
				IsBinary:  isBinary,
			})
		case *network.EventWebSocketFrameReceived:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			if wsURL == "" {
				return
			}
			isBinary := e.Response.Opcode == 2
			data := e.Response.PayloadData
			if isBinary {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "receive",
				Data:      data,
				Timestamp: time.Now(),
				Opcode:    e.Response.Opcode,
				IsBinary:  isBinary,
			})
		case *network.EventWebSocketFrameError:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "error",
				Data:      e.ErrorMessage,
				Timestamp: time.Now(),
			})
		}
	})
}

func (c *Crawler) fetchPageMetadata(ctx context.Context, urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	root := parsed.Scheme + "://" + parsed.Host
	host := parsed.Hostname()

	c.hostMu.Lock()
	if c.hostLastCrawl[host+"_meta"] != (time.Time{}) {
		c.hostMu.Unlock()
		return
	}
	c.hostLastCrawl[host+"_meta"] = time.Now()
	c.hostMu.Unlock()

	type metaFile struct {
		url  string
		mime string
	}
	files := []metaFile{
		{url: root + "/favicon.ico", mime: "image/x-icon"},
		{url: root + "/manifest.json", mime: "application/json"},
		{url: root + "/robots.txt", mime: "text/plain"},
	}

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(f metaFile) {
			defer wg.Done()

			etag, lastMod := "", ""
			if c.cfg.Incremental && c.incCache != nil {
				etag, lastMod = c.incCache.ConditionalHeaders(f.url)
			}

			// HEAD preflight to avoid downloading 404/301 responses
			headReq, err := http.NewRequestWithContext(ctx, "HEAD", f.url, nil)
			if err != nil {
				return
			}
			if etag != "" {
				headReq.Header.Set("If-None-Match", etag)
			}
			if lastMod != "" {
				headReq.Header.Set("If-Modified-Since", lastMod)
			}

			headResp, err := c.httpClient.Do(headReq)
			if err != nil {
				return
			}
			headResp.Body.Close()

			if headResp.StatusCode == 304 {
				return
			}
			if headResp.StatusCode != 200 {
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", f.url, nil)
			if err != nil {
				return
			}
			if etag != "" {
				req.Header.Set("If-None-Match", etag)
			}
			if lastMod != "" {
				req.Header.Set("If-Modified-Since", lastMod)
			}

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 304 {
				return
			}

			if resp.StatusCode != 200 {
				return
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil || len(body) == 0 {
				return
			}

			c.storage.SaveFile(f.url, body, f.mime)

			if c.cfg.Incremental && c.incCache != nil {
				headers := make(map[string]string)
				for k := range resp.Header {
					headers[k] = resp.Header.Get(k)
				}
				c.incCache.UpdateFromResponse(f.url, resp.StatusCode, headers)
			}
		}(f)
	}
	wg.Wait()
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
	for _, pattern := range c.cfg.ExcludePatterns {
		if strings.Contains(rawURL, pattern) {
			return true
		}
	}
	return false
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
		select {
		case <-time.After(c.cfg.WaitStrategyTimeout):
		case <-ctx.Done():
		}
	}
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

func (c *Crawler) solveCaptcha(tabCtx context.Context, urlStr string, html string) {
	if c.captchaSolver == nil || c.cfg.CAPTCHAConfig == nil || !c.cfg.CAPTCHAConfig.Enabled {
		return
	}
	if c.cfg.CAPTCHAConfig.Provider == "" {
		return
	}
	solved := false
	for attempt := 0; attempt < c.cfg.CAPTCHAConfig.RetryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt*2) * time.Second):
			case <-c.ctx.Done():
				return
			}
		}
		elems, exists := c.detectCaptchaElements(tabCtx, urlStr)
		if !exists {
			return
		}
		req := captcha.SolveRequest{
			URL:      urlStr,
			SiteKey:  elems["sitekey"],
			PageHTML: html,
		}
		if req.SiteKey == "" {
			return
		}
		resp, err := c.captchaSolver.Solve(tabCtx, req)
		if err != nil {
			util.LogDebug("captcha solve failed",
				zap.String("url", urlStr),
				zap.Error(err),
				zap.Int("attempt", attempt+1),
			)
			continue
		}
		if resp.Solved && resp.Token != "" {
			c.injectCaptchaToken(tabCtx, resp.Token, elems)
			solved = true
			time.Sleep(2 * time.Second)
			break
		}
	}
	if !solved {
		util.LogDebug("captcha not solved after retries", zap.String("url", urlStr))
	}
}

func (c *Crawler) detectCaptchaElements(tabCtx context.Context, urlStr string) (map[string]string, bool) {
	result := make(map[string]string)
	var scriptResult string
	script := `
		(function() {
			var result = {};

			// reCAPTCHA v2/v3
			var recaptcha = document.querySelector('.g-recaptcha, div[data-sitekey], [data-runtime="google/recaptcha"], iframe[src*="google.com/recaptcha"], .recaptcha');
			if (recaptcha) {
				result["sitekey"] = recaptcha.getAttribute("data-sitekey") || recaptcha.dataset.sitekey || "";
				result["type"] = "recaptcha";
				result["found"] = true;
			}

			// hCaptcha
			var hcaptcha = document.querySelector('.h-captcha, iframe[src*="hcaptcha.com"], div[data-sitekey][data-theme]');
			if (hcaptcha && !result.found) {
				result["sitekey"] = hcaptcha.getAttribute("data-sitekey") || "";
				result["type"] = "hcaptcha";
				result["found"] = true;
			}

			// Cloudflare Turnstile
			var turnstile = document.querySelector('.cf-turnstile, div[data-sitekey][data-appearance], iframe[src*="challenges.cloudflare.com"]');
			if (turnstile && !result.found) {
				result["sitekey"] = turnstile.getAttribute("data-sitekey") || "";
				result["type"] = "turnstile";
				result["found"] = true;
			}

			// Generic fallback: any element with data-sitekey attribute
			if (!result.found) {
				var allSiteKeys = document.querySelectorAll('[data-sitekey]');
				if (allSiteKeys.length > 0) {
					result["sitekey"] = allSiteKeys[0].getAttribute("data-sitekey") || "";
					result["type"] = "generic";
					result["found"] = true;
				}
			}

			// Check for loaded CAPTCHA scripts
			if (!result.found) {
				var scripts = document.querySelectorAll('script[src*="recaptcha"], script[src*="hcaptcha"], script[src*="challenges.cloudflare"]');
				if (scripts.length > 0) {
					result["type"] = "detected_via_script";
					result["found"] = true;
				}
			}

			return JSON.stringify(result);
		})()
	`
	err := chromedp.Run(tabCtx, chromedp.Evaluate(script, &scriptResult))
	if err != nil {
		return nil, false
	}
	if err := json.Unmarshal([]byte(scriptResult), &result); err != nil {
		return nil, false
	}
	siteKey, _ := result["sitekey"]
	if siteKey == "" {
		return nil, false
	}
	return result, true
}

func (c *Crawler) injectCaptchaToken(tabCtx context.Context, token string, elems map[string]string) {
	safeToken, _ := json.Marshal(token)
	tokenStr := string(safeToken)
	captchaType := elems["type"]
	switch captchaType {
	case "hcaptcha":
		_ = chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector('[data-hcaptcha-response]').innerHTML = %s;`, tokenStr), nil))
	case "turnstile":
		_ = chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector('[data-turnstile-response]').innerHTML = %s;`, tokenStr), nil))
	default:
		_ = chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`var e = document.getElementById("g-recaptcha-response"); if(e) e.innerHTML = %s;`, tokenStr), nil))
	}
}
