package crawler

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) captureCurrentPage(rawTabCtx context.Context, urlStr string, netIntercept *netintercept.Interceptor) (string, error) {

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

	if err := c.memoryBudget.ReserveBlocking(rawTabCtx, int64(len(html))); err != nil {
		return "", err
	}
	defer c.memoryBudget.Release(int64(len(html)))

	if c.changeDetector != nil {
		var title string
		chromedp.Run(captureCtx, chromedp.Title(&title))

		// Extract text content and structure for change detection
		textContent := c.extractTextContent(captureCtx)
		structure := c.extractPageStructure(captureCtx)

		// Capture new snapshot
		_, err := c.changeDetector.CaptureSnapshot(c.ctx, urlStr, html, textContent, structure, map[string]string{
			"title": title,
		})
		if err != nil {
			util.LogError("failed to capture snapshot", err, zap.String("url", urlStr))
		} else {
			// Compare with previous snapshot
			diff, err := c.changeDetector.CompareWithPrevious(c.ctx, urlStr)
			if err != nil {
				util.LogError("failed to compare snapshots", err, zap.String("url", urlStr))
			} else if diff.ChangeCount > 0 {
				util.LogInfo("page changed",
					zap.String("url", urlStr),
					zap.Int("changes", diff.ChangeCount),
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
			util.LogError("screenshot failed", err, zap.String("url", urlStr))
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
			util.LogError("pdf failed", err, zap.String("url", urlStr))
		}
	}

	htmlLocalPath, err := c.storage.SaveHTML(urlStr, []byte(html))
	htmlDir := filepath.Dir(htmlLocalPath)
	if err != nil {
		return html, err
	}

	c.rewriter.SetBaseURL(urlStr)

	// Initialize optimized rewriter on first use
	if c.optimizedRewriter != nil {
		c.optimizedRewriter.Initialize(htmlDir, urlStr)
	}

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

		localPath, saved := c.saveCapturedResource(origURL, resource.Body, resource.MimeType, htmlDir)
		if !saved {
			if c.cfg.Incremental && c.incCache != nil {
				c.incCache.UpdateFromResponse(origURL, int(resource.StatusCode), resource.Headers)
			}
			if localPath != "" {
				cdpSaved[origURL] = true
			}
			continue
		}

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
		if _, saved := c.saveCapturedResource(missingURL, resource.Body, resource.MimeType, htmlDir); saved {
			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(resource.Body)))
		}
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
									if _, copyErr := io.Copy(io.Discard, fontResp.Body); copyErr != nil {
										util.LogDebug("failed to discard font response body", zap.Error(copyErr), zap.String("url", absFontURL))
									}
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
								if _, copyErr := io.Copy(io.Discard, fontResp.Body); copyErr != nil {
									util.LogDebug("failed to discard font response body", zap.Error(copyErr), zap.String("url", absFontURL))
								}
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
								if _, copyErr := io.Copy(io.Discard, cssResp.Body); copyErr != nil {
									util.LogDebug("failed to discard CSS response body", zap.Error(copyErr), zap.String("url", absCSSURL))
								}
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
							if _, copyErr := io.Copy(io.Discard, cssResp.Body); copyErr != nil {
								util.LogDebug("failed to discard CSS response body", zap.Error(copyErr), zap.String("url", absCSSURL))
							}
							cssResp.Body.Close()
						}
					}
				}
			}
		}
	}

	c.resolveJSDependencies(htmlLocalPath, urlStr)

	return html, nil
}
