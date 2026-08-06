package crawler

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/browserpool"
	"github.com/user/clone/internal/pool"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// Start begins the crawl with the given seed URLs.
// It initializes the browser pool, starts the coordinator (if configured),
// and runs the main crawl loop until all URLs are processed or the crawl
// is stopped. The function blocks until the crawl completes or is interrupted.
// Returns an error if the crawl fails to start or encounters a fatal error.
func (c *Crawler) Start(seeds []string) error {
	if !c.started.CompareAndSwap(false, true) {
		return fmt.Errorf("crawl already running")
	}
	// Reset pause state from any previous crawl. A paused crawl that was
	// stopped (or whose context was cancelled) would otherwise leave paused
	// set, and the next crawl's dispatch loop would block in the pause wait
	// forever without an explicit Resume. Also drain any stale resume signal
	// left over in the buffered channel.
	c.paused.Store(false)
	select {
	case <-c.resumeCh:
	default:
	}
	// Refresh the seeds held on the crawler so helpers like isSeedPage (which
	// reads c.seeds) work for crawls started via the REST API as well as the
	// CLI. NewCrawler already copies cfg.Seeds, but the API passes seeds that
	// are not on the config; a copy is stored (not the caller's slice) and the
	// write is synchronized with Status()/isSeedPage reads, since the API
	// starts crawls in a goroutine that can overlap /api/status polling.
	c.seedsMu.Lock()
	c.seeds = append([]string{}, seeds...)
	c.seedsMu.Unlock()
	defer c.started.Store(false)
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
			// Rehydrate the in-memory dedup layers from the checkpoint visited
			// set so resume does not double-count MaxTotalURLs or re-add URLs.
			for u := range data.Visited {
				c.exactDedup.Add(u)
				c.bloomFilter.Add(u)
			}
			util.LogInfo("resumed from checkpoint",
				zap.Int("queue_size", len(data.Queue)),
				zap.Int("visited_count", len(data.Visited)),
			)
		}
	}

	c.queueMu.Lock()
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
	queueEmpty := c.urlQueue.Size() == 0
	c.queueMu.Unlock()

	if queueEmpty {
		return fmt.Errorf("no valid http/https seed URLs provided; got %d seed(s): %v", len(seeds), seeds)
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
	// Mobile emulation support
	if c.cfg.MobileEmulation {
		deviceName := c.cfg.MobileDevice
		if deviceName == "" {
			deviceName = "iPhone 12"
		}
		width := int64(c.cfg.ViewportWidth)
		height := int64(c.cfg.ViewportHeight)
		deviceScaleFactor := float64(1)
		mobile := true
		c.mobileEmulationParams = &emulation.SetDeviceMetricsOverrideParams{
			Width:             width,
			Height:            height,
			DeviceScaleFactor: deviceScaleFactor,
			Mobile:            mobile,
		}
		c.mobileTouchParams = &emulation.SetTouchEmulationEnabledParams{
			Enabled: true,
		}
		if c.cfg.MobileUserAgent != "" {
			c.allocOpts = append(c.allocOpts, chromedp.UserAgent(c.cfg.MobileUserAgent))
		}
	}
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

	c.browserPool = browserpool.New(c.ctx, c.cfg.BrowserPoolSize, c.allocOpts, c.cfg.RemoteChromeURL)
	if err := c.browserPool.Start(); err != nil {
		return fmt.Errorf("browser pool start failed: %w", err)
	}

	// Start coordinator for distributed worker coordination
	if c.coordinator != nil {
		if err := c.coordinator.Start(c.ctx); err != nil {
			util.LogError("failed to start coordinator", err)
		} else {
			// Register leader change callback
			c.coordinator.WatchLeaderChange(c.ctx, func(leaderID string) {
				util.LogInfo("leader changed", zap.String("leader_id", leaderID), zap.Bool("is_leader", leaderID == c.coordinator.LeaderID()))
			})
		}
	}

	go c.periodicCheckpoint()
	go c.reportProgress()
	go c.periodicCleanup()
	go c.periodicCookieSave()
	go c.periodicBrowserHealthCheck()
	go c.periodicMetricsUpdate()
	go c.periodicDiskSpaceCheck()

	c.loadCookieJar()

	if c.cfg.Interactive && c.cfg.AuthConfig != nil && c.cfg.AuthConfig.Enabled && c.cfg.AuthConfig.LoginURL != "" {
		fmt.Println("\n=== Pre-crawl Login Phase ===")
		fmt.Printf("Login URL: %s\n", c.cfg.AuthConfig.LoginURL)
		fmt.Println("Log in manually in the browser, then press Enter to begin crawling.")
		fmt.Print("Press Enter when ready (or 'q' to quit): ")

		loginTabCtx, err := c.browserPool.Acquire()
		if err != nil {
			c.closeWriters()
			return fmt.Errorf("failed to acquire browser for login: %w", err)
		}
		loginCtx, loginTimeoutCancel := context.WithTimeout(loginTabCtx, 30*time.Second)
		defer loginTimeoutCancel()
		if err := chromedp.Run(loginCtx, chromedp.Navigate(c.cfg.AuthConfig.LoginURL)); err != nil {
			c.closeWriters()
			return fmt.Errorf("failed to navigate to login URL: %w", err)
		}
		c.waitForPage(loginTabCtx)

		var input string
		if _, err := fmt.Scanln(&input); err == nil {
			if input == "q" || input == "Q" || input == "quit" || input == "exit" {
				c.closeWriters()
				util.LogInfo("user cancelled during pre-crawl login")
				return nil
			}
		}
		c.persistCookies(loginTabCtx, c.cfg.AuthConfig.LoginURL)
		fmt.Println("Login captured. Starting crawl...")
	}

	util.LogInfo("starting crawl",
		zap.Int("seeds", len(seeds)),
		zap.Int("max_concurrent", c.cfg.MaxConcurrentPages),
		zap.Int("max_depth", c.cfg.MaxDepth),
		zap.Int("max_urls_per_host", c.cfg.MaxURLsPerHost),
		zap.String("wait_strategy", c.cfg.WaitStrategy),
		zap.Bool("infinite_scroll", c.cfg.InfiniteScroll != nil && c.cfg.InfiniteScroll.Enabled),
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
		capCtx, err := c.browserPool.Acquire()
		if err != nil {
			return fmt.Errorf("failed to acquire browser for manual capture: %w", err)
		}
		c.manualCapture(capCtx, seeds)
		goto cleanup
	}

Loop:
	for {
		select {
		case <-c.ctx.Done():
			break Loop
		default:
		}

		// Pause support: while paused, stop dispatching new URLs (already-
		// running pages finish normally) and wait for Resume to unblock.
		for c.paused.Load() {
			select {
			case <-c.resumeCh:
			case <-c.ctx.Done():
				break Loop
			}
		}

		if c.cfg.MaxTotalURLs > 0 && c.totalURLs.Load() >= int64(c.cfg.MaxTotalURLs) {
			util.LogInfo("reached total URL limit", zap.Int("limit", c.cfg.MaxTotalURLs))
			break
		}

		c.queueMu.RLock()
		item, ok := c.urlQueue.PopURL()
		c.queueMu.RUnlock()
		if !ok {

			if c.activePages.Load() == 0 {
				break
			}
			waitTimer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-waitTimer.C:
			case <-c.ctx.Done():
				if !waitTimer.Stop() {
					<-waitTimer.C
				}
				break Loop
			}
			continue
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

		isAsset := !isHTMLPageURL(item.URL)
		if isAsset {
			if c.blockedPatternMatch(item.URL) {
				continue
			}
			// Keep the per-host circuit breaker in the asset path so a failing
			// host is not hammered by parallel unbounded asset downloads.
			if !c.circuitBreaker.Allow(host) {
				util.LogDebug("circuit open, skipping asset", zap.String("host", host))
				continue
			}
			c.wg.Add(1)
			select {
			case c.assetSem <- struct{}{}:
			case <-c.ctx.Done():
				c.wg.Done()
				break Loop
			}
			c.activePages.Add(1)
			go func(url string) {
				defer c.wg.Done()
				defer c.activePages.Add(-1)
				defer func() { <-c.assetSem }()
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
				c.crawlAsset(c.ctx, url)
			}(item.URL)
			continue
		}

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

		c.activePages.Add(1)
		go func(url string, depth int, hst string, semCh chan struct{}, start time.Time) {
			defer c.wg.Done()
			defer c.activePages.Add(-1)
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
	c.writeMappingFile()

	apiResponses := c.apiResponses.GetAll()
	wsRaw := c.wsMessages.GetAll()
	c.writeAPIResponses(apiResponses)
	c.writeHAR(apiResponses)
	c.writeSW(apiResponses, wsRaw)
	c.writeSitemap()
	c.saveCookieJar()
	c.closeWriters()

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

	if c.cfg.CheckpointFile != "" && c.checkpoint.Exists() {
		if err := c.checkpoint.Remove(); err == nil {
			util.LogDebug("removed completed crawl checkpoint")
		}
	}
	if c.cfg.BloomFilterPath != "" {
		_ = os.Remove(c.cfg.BloomFilterPath)
	}

	if c.cfg.Incremental && c.incCache != nil {
		if err := c.incCache.Save(); err != nil {
			util.LogError("failed to save incremental cache", err)
		}
	}
	return nil
}
