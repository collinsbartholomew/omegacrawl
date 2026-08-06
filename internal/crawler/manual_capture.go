package crawler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/jsengine"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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
				util.LogError("failed to save API response", err, zap.String("url", ar.URL))
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
