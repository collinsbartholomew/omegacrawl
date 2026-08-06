package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	crawlerrors "github.com/user/clone/internal/errors"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/tracing"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) doCrawl(browserCtx context.Context, urlStr string, depth int) error {
	// Start trace span for page crawl
	ctx, span := tracing.StartSpan(browserCtx, "crawler.doCrawl",
		tracing.WithAttribute("url", urlStr),
		tracing.WithAttribute("depth", depth),
	)
	defer span.End()

	rawTabCtx, tabCancel := chromedp.NewContext(ctx)
	defer tabCancel()

	// Apply mobile emulation to this tab if enabled
	if c.cfg.MobileEmulation && c.mobileEmulationParams != nil {
		if err := chromedp.Run(rawTabCtx,
			c.mobileEmulationParams,
			c.mobileTouchParams,
		); err != nil {
			util.LogDebug("mobile emulation setup failed", zap.Error(err))
			span.RecordError(err)
		}
	}

	tabCtx, tabCancel2 := context.WithTimeout(rawTabCtx, c.cfg.PageTimeout)
	defer tabCancel2()

	if c.cfg.EnableStealth {
		if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(jsengine.StealthScript).Do(ctx)
			return err
		})); err != nil {
			util.LogDebug("stealth injection failed", zap.Error(err))
			span.RecordError(err)
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
		patterns := make([]*network.BlockPattern, len(c.cfg.BlockedURLPatterns))
		for i, p := range c.cfg.BlockedURLPatterns {
			patterns[i] = &network.BlockPattern{URLPattern: p, Block: true}
		}
		if err := chromedp.Run(tabCtx, network.SetBlockedURLs().WithURLPatterns(patterns)); err != nil {
			util.LogDebug("failed to set blocked URL patterns", zap.Error(err))
		}
	}

	if !c.cfg.Interactive && c.authManager != nil && c.cfg.AuthConfig != nil && c.cfg.AuthConfig.Enabled {
		if err := c.authManager.Authenticate(tabCtx, urlStr); err != nil {
			util.LogError("authentication failed", err, zap.String("url", urlStr))
		}
	}

	// Acquire interceptor from pool
	workerCount := c.cfg.MaxConcurrentPages * 2
	if workerCount < 5 {
		workerCount = 5
	}
	netIntercept, err := c.interceptorPool.Acquire(tabCtx)
	if err != nil {
		return err
	}
	defer c.interceptorPool.Release(netIntercept)

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
				util.LogError("failed to save API response", err, zap.String("url", ar.URL))
			}
		}
	})
	netIntercept.Start(tabCtx, urlStr)

	for _, js := range c.cfg.JSBeforeLoad {
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, nil)); err != nil {
			util.LogDebug("js before load failed", zap.Error(err))
			span.RecordError(err)
		}
	}

	// Navigate with tracing
	navCtx, navSpan := tracing.StartSpan(tabCtx, "crawler.navigate",
		tracing.WithAttribute("url", urlStr),
	)
	err = chromedp.Run(navCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errorText, _, err := page.Navigate(urlStr).Do(ctx)
		if err != nil {
			return err
		}
		if errorText != "" {
			return crawlerrors.Wrap(crawlerrors.KindBrowser, "navigation failed", fmt.Errorf("%s", errorText))
		}
		return nil
	}))
	navSpan.End()
	if err != nil {
		span.RecordError(err)
		return err
	}

	_, waitSpan := tracing.StartSpan(tabCtx, "crawler.waitForPage")
	c.waitForPage(tabCtx)
	waitSpan.End()

	var finalURL string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(`window.location.href`, &finalURL)); err != nil {
		util.LogDebug("failed to get final URL", zap.Error(err))
		span.RecordError(err)
	}
	if finalURL != "" && finalURL != urlStr {
		util.LogDebug("redirected",
			zap.String("from", urlStr),
			zap.String("to", finalURL),
		)
		span.SetAttributes(tracing.AttrString("final_url", finalURL))
	}
	c.persistCookies(tabCtx, urlStr)

	_, strategySpan := tracing.StartSpan(tabCtx, "crawler.applyWaitStrategy",
		tracing.WithAttribute("strategy", c.cfg.WaitStrategy),
	)
	c.applyWaitStrategy(tabCtx)
	strategySpan.End()

	if c.cfg.DismissOverlays {
		_, overlaySpan := tracing.StartSpan(tabCtx, "crawler.dismissOverlays")
		jsengine.DismissOverlays(tabCtx)
		overlaySpan.End()
	}
	if c.cfg.ExpandSections {
		_, expandSpan := tracing.StartSpan(tabCtx, "crawler.expandSections")
		jsengine.ExpandAllSections(tabCtx)
		expandSpan.End()
	}
	for _, selector := range c.cfg.ClickSelectors {
		_, clickSpan := tracing.StartSpan(tabCtx, "crawler.clickElement",
			tracing.WithAttribute("selector", selector),
		)
		jsengine.ClickElement(tabCtx, selector)
		clickTimer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-clickTimer.C:
		case <-c.ctx.Done():
			if !clickTimer.Stop() {
				<-clickTimer.C
			}
			clickSpan.End()
			return nil
		}
		clickSpan.End()
	}
	if c.cfg.EnableLazyLoad {
		_, lazySpan := tracing.StartSpan(tabCtx, "crawler.injectLazyLoad")
		jsengine.InjectLazyLoad(tabCtx)
		lazySpan.End()
	}

	if c.cfg.InfiniteScroll != nil && c.cfg.InfiniteScroll.Enabled {
		_, scrollSpan := tracing.StartSpan(tabCtx, "crawler.infiniteScroll",
			tracing.WithAttribute("max_scrolls", c.cfg.InfiniteScroll.MaxScrolls),
			tracing.WithAttribute("max_duration", c.cfg.InfiniteScroll.MaxDuration.String()),
		)
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
		scrollSpan.End()
	}

	for _, js := range c.cfg.JSAfterLoad {
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, nil)); err != nil {
			util.LogDebug("js after load failed", zap.Error(err))
			span.RecordError(err)
		}
	}

	if c.cfg.Interactive {
		_, interactiveSpan := tracing.StartSpan(tabCtx, "crawler.interactivePrompt")
		c.promptUser(tabCtx, urlStr)
		interactiveSpan.End()
	}

	if c.cfg.EnableInteractionEngine {
		_, interactSpan := tracing.StartSpan(tabCtx, "crawler.runInteractionEngine")
		c.runInteractionEngine(tabCtx, urlStr)
		interactSpan.End()
	}

	if c.cfg.EnableRouteDiscovery {
		_, routeSpan := tracing.StartSpan(tabCtx, "crawler.discoverRoutes")
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
			routeSpan.SetAttributes(
				tracing.AttrInt("pushstate_routes", len(pushRoutes)),
				tracing.AttrInt("new_routes", len(routesToCrawl)),
			)

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
				spaNavCtx, spaNavSpan := tracing.StartSpan(navCtx, "crawler.spaRouteNavigation",
					tracing.WithAttribute("route", routeURL),
				)
				err := chromedp.Run(spaNavCtx,
					chromedp.ActionFunc(func(ctx context.Context) error {
						_, _, errorText, _, err := page.Navigate(routeURL).Do(ctx)
						if err != nil {
							return err
						}
						if errorText != "" {
							return fmt.Errorf("navigation error: %s", errorText)
						}
						return nil
					}),
				)
				spaNavSpan.End()
				navCancel()
				if err != nil {
					util.LogDebug("failed to navigate to SPA route",
						zap.String("route", routeURL),
						zap.Error(err),
					)
					span.RecordError(err)
					continue
				}

				waitCtx, waitCancel := context.WithTimeout(tabCtx, 15*time.Second)
				chromedp.Run(waitCtx, chromedp.WaitReady("body", chromedp.ByQuery))

				jsengine.WaitForNetworkIdle(waitCtx, 1*time.Second)

				chromedp.Sleep(2 * time.Second).Do(waitCtx)
				waitCancel()

				// Capture the rendered HTML
				var spaHTML string
				if err := chromedp.Run(tabCtx, chromedp.OuterHTML("html", &spaHTML)); err != nil || spaHTML == "" {
					continue
				}

				spaPath, err := c.storage.SaveHTML(routeURL, []byte(spaHTML))
				if err != nil {
					util.LogError("failed to save SPA route HTML", err, zap.String("route", routeURL))
					continue
				}
				c.rewriter.AddMapping(routeURL, spaPath)
				// Capture the assets referenced by the route HTML and resolve its
				// JS dependencies so the localized pass can rewrite the route
				// fully, matching the main page pipeline.
				c.downloadHTMLAssets(routeURL, spaHTML, spaPath, netIntercept, nil)
				c.resolveJSDependencies(spaPath, routeURL)
				util.LogDebug("captured SPA route", zap.String("route", routeURL), zap.String("path", spaPath))
			}
		}

		if err := chromedp.Run(tabCtx, chromedp.Navigate(urlStr)); err == nil {
			c.waitForPage(tabCtx)
		}
		routeSpan.End()
	}

	if c.cfg.EnableShadowDOM {
		_, shadowSpan := tracing.StartSpan(tabCtx, "crawler.extractShadowDOM")
		shadowInfo, err := jsengine.ExtractShadowDOM(tabCtx)
		if err == nil && shadowInfo != nil && shadowInfo.Count > 0 {
			data, err := json.Marshal(shadowInfo)
			if err != nil {
				util.LogError("failed to marshal shadow DOM", err)
				shadowSpan.RecordError(err)
			} else {
				c.storage.SaveShadowDOM(urlStr, data)
				shadowSpan.SetAttributes(tracing.AttrInt("shadow_dom_count", shadowInfo.Count))
			}
		}
		shadowSpan.End()
	}

	var structuredData []byte
	if c.cfg.EnableStructuredData {
		_, sdSpan := tracing.StartSpan(tabCtx, "crawler.extractStructuredData")
		sd, err := jsengine.ExtractStructuredData(tabCtx)
		if err == nil && sd != nil && (len(sd.JSONLD) > 0 || len(sd.OG) > 0 || len(sd.Twitter) > 0 || len(sd.Meta) > 0) {
			structuredData, err = json.Marshal(sd)
			if err == nil {
				// Per-page file: every page keeps its own structured data.
				if p, perr := c.storage.SaveStructuredData(urlStr, structuredData); perr != nil {
					util.LogError("failed to save structured data", perr, zap.String("url", urlStr))
					sdSpan.RecordError(perr)
				} else {
					util.LogDebug("saved structured data", zap.String("path", p))
				}
				// Site-root aggregate consumed by the downstream Next.js importer
				// as the site-level document (seed page preferred; falls back to
				// the first page with content so the file is not missing when the
				// seed page itself has no structured data).
				c.writeSiteAggregate(urlStr, "structured-data.json", structuredData, &c.siteSDWritten)
				sdSpan.SetAttributes(
					tracing.AttrBool("has_jsonld", len(sd.JSONLD) > 0),
					tracing.AttrBool("has_og", len(sd.OG) > 0),
					tracing.AttrBool("has_twitter", len(sd.Twitter) > 0),
					tracing.AttrBool("has_meta", len(sd.Meta) > 0),
				)
			}
		}
		sdSpan.End()
	}

	if c.cfg.EnableStealth {
		_, swSpan := tracing.StartSpan(tabCtx, "crawler.serviceWorkerCleanup")
		swMgr := jsengine.NewServiceWorkerManager()
		swMgr.Detect(tabCtx)
		swMgr.Unregister(tabCtx)
		swSpan.End()
	}

	if c.cfg.EnableArticleExtract {
		_, articleSpan := tracing.StartSpan(tabCtx, "crawler.extractArticle")
		article, err := jsengine.ExtractArticle(tabCtx)
		if err == nil && article != nil && article.Content != "" {
			article.URL = urlStr
			article.ExtractedAt = time.Now().Format(time.RFC3339)
			articleData, _ := json.MarshalIndent(article, "", "  ")
			// Per-page file: every page keeps its own article extraction.
			if p, perr := c.storage.SaveArticle(urlStr, articleData); perr != nil {
				util.LogError("failed to save article", perr, zap.String("url", urlStr))
				articleSpan.RecordError(perr)
			} else {
				util.LogDebug("saved article", zap.String("path", p))
			}
			// Site-root aggregate for the site-level document (seed page
			// preferred; falls back to the first page with content).
			c.writeSiteAggregate(urlStr, "article.json", articleData, &c.siteArticleWritten)
			articleSpan.SetAttributes(
				tracing.AttrInt("article_length", len(article.Content)),
				tracing.AttrString("article_title", article.Title),
			)
		}
		articleSpan.End()
	}

	if c.cfg.EnableSingleFile {
		_, singleFileSpan := tracing.StartSpan(tabCtx, "crawler.generateSingleFile")
		singleFile, err := jsengine.GenerateSingleFile(tabCtx)
		if err == nil && singleFile != "" {
			if _, perr := c.storage.SaveSingleFile(urlStr, []byte(singleFile)); perr != nil {
				util.LogError("failed to save single file", perr, zap.String("url", urlStr))
				singleFileSpan.RecordError(perr)
			}
		}
		singleFileSpan.End()
	}

	html, err := c.captureCurrentPage(rawTabCtx, urlStr, netIntercept)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(tracing.AttrInt("html_size_bytes", len(html)))

	_, linksSpan := tracing.StartSpan(ctx, "crawler.extractLinks")
	links := c.rewriter.ExtractLinks(urlStr, []byte(html))
	c.routeMu.RLock()
	for route := range c.discoveredRoutes {
		if c.isSameDomain(urlStr, route) {
			links = append(links, route)
		}
	}
	c.routeMu.RUnlock()
	linksSpan.SetAttributes(tracing.AttrInt("link_count", len(links)))
	linksSpan.End()

	_, queueSpan := tracing.StartSpan(ctx, "crawler.queueLinks")
	for _, link := range links {
		normalized := c.normalizeURL(queue.NormalizeAndClean(link))
		if c.shouldQueue(normalized) {
			c.urlQueue.PushURL(normalized, depth+1)
		}
	}
	queueSpan.End()

	if c.cfg.EnableIframes && depth < c.cfg.MaxIframeDepth {
		_, iframeSpan := tracing.StartSpan(tabCtx, "crawler.extractIframes")
		iframeSources, err := jsengine.ExtractIframeSources(tabCtx)
		if err == nil && len(iframeSources) > 0 {
			util.LogDebug("discovered iframes",
				zap.String("url", urlStr),
				zap.Int("count", len(iframeSources)),
			)
			iframeSpan.SetAttributes(tracing.AttrInt("iframe_count", len(iframeSources)))
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
		iframeSpan.End()
	}

	if c.cfg.EnableMediaCapture {
		_, mediaSpan := tracing.StartSpan(tabCtx, "crawler.extractMedia")
		mediaSources, err := jsengine.ExtractMediaSources(tabCtx)
		if err == nil && len(mediaSources) > 0 {
			util.LogDebug("discovered media sources",
				zap.String("url", urlStr),
				zap.Int("count", len(mediaSources)),
			)
			mediaSpan.SetAttributes(tracing.AttrInt("media_count", len(mediaSources)))
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
		mediaSpan.End()
	}

	pageCtx, pageCancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer pageCancel()
	_, metaSpan := tracing.StartSpan(pageCtx, "crawler.fetchMetadata")
	c.fetchPageMetadata(pageCtx, urlStr)
	metaSpan.End()

	netIntercept.Close()

	span.SetAttributes(
		tracing.AttrInt("total_queued", len(links)),
	)
	return nil
}

// writeSiteAggregate writes a host-level aggregate file (article.json /
// structured-data.json) consumed by downstream importers as the site-level
// document. The seed page is the preferred source and always wins; if the
// seed page produced no content, the first page that does fills the slot so
// the file is still produced. The written flag keeps concurrent page
// goroutines from clobbering each other's fallback writes.
func (c *Crawler) writeSiteAggregate(urlStr, name string, data []byte, written *atomic.Bool) {
	isSeed := c.isSeedPage(urlStr)
	if !isSeed {
		// Non-seed pages only fill the slot if no aggregate exists yet. The
		// slot stays claimed even if the write below fails: releasing it would
		// race with a seed page legitimately setting the flag, and a failing
		// write would likely fail again on retry.
		if !written.CompareAndSwap(false, true) {
			return
		}
	}
	savePath := c.cfg.OutputDir + "/" + getHost(urlStr) + "/" + name
	os.MkdirAll(filepath.Dir(savePath), 0755)
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		util.LogError("failed to save site "+name, err, zap.String("path", savePath))
		return
	}
	if isSeed {
		// Mark the seed aggregate as written only on success, so a failed
		// write still leaves the fallback available for a later page.
		written.Store(true)
	}
}
