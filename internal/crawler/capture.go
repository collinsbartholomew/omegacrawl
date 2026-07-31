package crawler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/jsanalyzer"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

func (c *Crawler) captureCurrentPage(rawTabCtx context.Context, urlStr string, netIntercept *netintercept.Interceptor) (string, error) {
	// Create a fresh sub-context for capture with its own timeout
	// to avoid the main captureCtx timing out during long capture operations
	captureCtx, captureCancel := context.WithTimeout(rawTabCtx, 30*time.Second)
	defer captureCancel()

	var html string
	if err := chromedp.Run(captureCtx, chromedp.Evaluate(`
		(function() {
			function serializeShadowDOM(root) {
				var elements = root.querySelectorAll('*');
				elements.forEach(function(el) {
					if (el.shadowRoot) {
						var template = document.createElement('template');
						template.innerHTML = '<template shadowrootmode="open">' + el.shadowRoot.innerHTML + '</template>';
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
		chromedp.Run(captureCtx, chromedp.Evaluate(`
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
			chromedp.Run(captureCtx, chromedp.OuterHTML("html", &html))
			if html != "" {
				html = "<!DOCTYPE html>\n" + html
			}
		}
	}
	if html == "" {
		chromedp.Run(captureCtx, chromedp.Evaluate(`document.body ? document.body.innerHTML : ''`, &html))
	}

	c.memoryBudget.ReserveBlocking(int64(len(html)))
	defer c.memoryBudget.Release(int64(len(html)))

	if c.changeDetector != nil {
		var title string
		chromedp.Run(captureCtx, chromedp.Title(&title))
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
		c.solveCaptcha(captureCtx, urlStr, html)
	}

	c.metrics.IncPagesFetched()

	framework, _ := jsengine.DetectFramework(captureCtx)
	if framework != nil {
		util.LogDebug("framework",
			zap.String("url", urlStr),
			zap.String("name", framework.Framework),
		)
	}

	var screenshot []byte
	if c.cfg.EnableScreenshot {
		if err := chromedp.Run(captureCtx, chromedp.FullScreenshot(&screenshot, 80)); err != nil {
			util.LogDebug("screenshot failed", zap.Error(err))
		}
	}

	var pdfData []byte
	if c.cfg.EnablePDF {
		if err := chromedp.Run(captureCtx,
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
		return html, err
	}

	c.rewriter.SetBaseURL(urlStr)

	c.writeRecord(&storage.WARCRecord{
		URL:        urlStr,
		Body:       []byte(html),
		MimeType:   "text/html",
		Date:       time.Now(),
		StatusCode: 200,
		RecordType: "response",
		ContentLen: int64(len(html)),
	})

	if screenshot != nil {
		c.storage.SaveScreenshot(urlStr, screenshot)
	}
	if pdfData != nil {
		c.storage.SavePDF(urlStr, pdfData)
	}

	netIntercept.FetchBodies(rawTabCtx)

	cdpSaved := make(map[string]bool)
	for origURL, resource := range netIntercept.GetResources() {
		if c.cfg.Incremental && resource.StatusCode == 304 {
			continue
		}

		hashStr := strconv.FormatUint(xxhash.Sum64(resource.Body), 36)

		added := c.contentHashes.AddIfAbsent(hashStr)

		if !added {
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

		c.writeRecord(&storage.WARCRecord{
			URL:        origURL,
			Body:       resource.Body,
			MimeType:   resource.MimeType,
			Date:       time.Now(),
			StatusCode: 200,
			RecordType: "response",
			ContentLen: int64(len(resource.Body)),
		})

		if c.cfg.Incremental && c.incCache != nil {
			c.incCache.UpdateFromResponse(origURL, int(resource.StatusCode), resource.Headers)
		}
		cdpSaved[origURL] = true
	}

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
		if !c.contentHashes.AddIfAbsent(hashStr) {
			continue
		}
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

	if html != "" {
		c.downloadHTMLAssets(urlStr, html, htmlLocalPath, netIntercept, cdpSaved)
	}

	assetCtx, assetCancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer assetCancel()

	for cssPath := range c.rewriter.GetCSSFiles() {
		cssData, err := os.ReadFile(cssPath)
		if err == nil {
			fontURLs := c.rewriter.ExtractFontURLs(cssData)
			for _, fontURL := range fontURLs {
				absFontURL := rewrite.ResolveURL(urlStr, fontURL)
				if absFontURL != "" && isValidURL(absFontURL) {
					if !c.bloomFilter.HasSeen(absFontURL) {
						c.bloomFilter.Add(absFontURL)
						fontReq, _ := http.NewRequestWithContext(assetCtx, "GET", absFontURL, nil)
						if fontReq != nil {
							fontResp, err := c.httpClient.Do(fontReq)
							if err != nil {
								if fontResp != nil {
									io.Copy(io.Discard, fontResp.Body)
									fontResp.Body.Close()
								}
							} else if fontResp.StatusCode == 200 {
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
							} else {
								io.Copy(io.Discard, fontResp.Body)
								fontResp.Body.Close()
							}
						}
					}
				}
			}

			cssURLs := c.rewriter.ExtractAllCSSURLs(cssData)
			for _, cssURL := range cssURLs {
				absCSSURL := rewrite.ResolveURL(urlStr, cssURL)
				if absCSSURL != "" && isValidURL(absCSSURL) && !c.bloomFilter.HasSeen(absCSSURL) {
					c.bloomFilter.Add(absCSSURL)
					cssReq, _ := http.NewRequestWithContext(assetCtx, "GET", absCSSURL, nil)
						if cssReq != nil {
							cssResp, err := c.httpClient.Do(cssReq)
							if err != nil {
								if cssResp != nil {
									io.Copy(io.Discard, cssResp.Body)
									cssResp.Body.Close()
								}
							} else if cssResp.StatusCode == 200 {
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
							} else {
								io.Copy(io.Discard, cssResp.Body)
								cssResp.Body.Close()
}
				}
			}
		}
	}
	}

	for cssPath := range c.rewriter.GetCSSFiles() {
		c.rewriter.ProcessFiles(map[string]string{cssPath: "css"})
	}

	c.resolveJSDependencies(htmlLocalPath, urlStr)
	c.rewriter.ProcessFiles(map[string]string{htmlLocalPath: "html"})

	return html, nil
}

func (c *Crawler) manualCapture(browserCtx context.Context, seedURLs []string) {
	rawTabCtx, tabCancel := chromedp.NewContext(browserCtx)
	defer tabCancel()

	captureCtx, tabCancel2 := context.WithTimeout(rawTabCtx, 24*time.Hour)
	defer tabCancel2()

	netIntercept := netintercept.NewInterceptorWithWorkers(5)
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

	startURL := ""
	if len(seedURLs) > 0 {
		startURL = seedURLs[0]
		chromedp.Run(captureCtx, chromedp.Navigate(startURL))
	}

	netIntercept.Start(captureCtx, startURL)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "q" || line == "Q" {
				c.Stop()
				return
			}
		}
	}()

	var lastURL string
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			var currentURL string
			err := chromedp.Run(captureCtx, chromedp.Evaluate("window.location.href", &currentURL))
			if err != nil || currentURL == "" || currentURL == "about:blank" {
				continue
			}
			if currentURL == lastURL {
				continue
			}
			lastURL = currentURL

			waitCtx, waitCancel := context.WithTimeout(captureCtx, c.cfg.PageTimeout)
			chromedp.Run(waitCtx, chromedp.WaitReady("body", chromedp.ByQuery))
			jsengine.WaitForNetworkIdle(waitCtx, 1*time.Second)
			chromedp.Sleep(500 * time.Millisecond).Do(waitCtx)
			waitCancel()

			if _, err := c.captureCurrentPage(rawTabCtx, currentURL, netIntercept); err != nil {
				util.LogError("failed to capture page", err, zap.String("url", currentURL))
			} else {
				fmt.Printf("  Captured: %s\n", currentURL)
			}
		}
	}
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
		if c.bloomFilter.HasSeen(assetURL) {
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
			if !c.contentHashes.AddIfAbsent(hashStr) {
				return nil
			}

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
			if c.bloomFilter.HasSeen(au.URL) {
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
			if !c.contentHashes.AddIfAbsent(hashStr) {
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

			if strings.HasSuffix(au.URL, ".js") {
				c.resolveJSDependenciesRecursive(au.URL, baseURL, htmlDir, 0)
			}
		}

		htmlURLs := jsanalyzer.ExtractFromHTML(string(jsData), baseURL)
		for _, au := range htmlURLs {
			if c.bloomFilter.HasSeen(au.URL) {
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

	if c.bloomFilter.HasSeen(jsURL) {
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
	if !c.contentHashes.AddIfAbsent(hashStr) {
		return
	}

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
			if c.bloomFilter.HasSeen(au.URL) {
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
			if !c.contentHashes.AddIfAbsent(hashStr2) {
				continue
			}

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
