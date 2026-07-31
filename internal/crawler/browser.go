package crawler

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) periodicBrowserHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if c.browserPool != nil {
				c.browserPool.HealthCheck()
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Crawler) launchBrowser() error {
	c.browserMu.Lock()
	defer c.browserMu.Unlock()

	if c.browserCancel != nil {
		c.browserCancel()
	}

	var allocCtx context.Context
	var allocCancel context.CancelFunc
	var browserCtx context.Context
	var browserCancel context.CancelFunc

	if c.cfg.RemoteChromeURL != "" {
		// Connect to an existing remote Chrome instance
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(c.ctx, c.cfg.RemoteChromeURL)
		browserCtx, browserCancel = chromedp.NewContext(allocCtx)

		navCtx, navCancel := context.WithTimeout(browserCtx, 30*time.Second)
		defer navCancel()
		if err := chromedp.Run(navCtx, chromedp.Navigate("about:blank")); err != nil {
			allocCancel()
			browserCancel()
			return fmt.Errorf("remote browser connection failed: %w", err)
		}
	} else {
		allocCtx, allocCancel = chromedp.NewExecAllocator(c.ctx, c.allocOpts...)
		browserCtx, browserCancel = chromedp.NewContext(allocCtx)

		navCtx, navCancel := context.WithTimeout(browserCtx, 30*time.Second)
		defer navCancel()
		if err := chromedp.Run(navCtx, chromedp.Navigate("about:blank")); err != nil {
			allocCancel()
			browserCancel()
			return fmt.Errorf("browser launch failed: %w", err)
		}
	}

	c.browserCtx = browserCtx
	c.browserCancel = func() {
		allocCancel()
		browserCancel()
		if c.cfg.RemoteChromeURL == "" {
			// Only wait for local Chrome to exit
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer waitCancel()
			select {
			case <-allocCtx.Done():
			case <-waitCtx.Done():
			}
		}
	}
	return nil
}

func (c *Crawler) getBrowserCtx() context.Context {
	c.browserMu.Lock()
	defer c.browserMu.Unlock()
	return c.browserCtx
}

func (c *Crawler) setupMobileEmulation(ctx context.Context) {
	mc := c.cfg.MobileEmulation
	if mc == nil {
		return
	}

	width := mc.Width
	if width <= 0 {
		width = c.cfg.ViewportWidth
	}
	height := mc.Height
	if height <= 0 {
		height = c.cfg.ViewportHeight
	}
	scale := mc.DeviceScaleFactor
	if scale <= 0 {
		scale = 1
	}

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := emulation.SetDeviceMetricsOverride(int64(width), int64(height), scale, mc.Mobile).Do(ctx); err != nil {
			return err
		}
		if mc.UserAgent != "" {
			return emulation.SetUserAgentOverride(mc.UserAgent).Do(ctx)
		}
		return nil
	}))
	if err != nil {
		util.LogDebug("mobile emulation setup failed", zap.Error(err))
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
			if len(data) > maxWSFrameSize {
				data = data[:maxWSFrameSize]
			}
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
			if len(data) > maxWSFrameSize {
				data = data[:maxWSFrameSize]
			}
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
				if headResp != nil {
					io.Copy(io.Discard, headResp.Body)
					headResp.Body.Close()
				}
				return
			}
			io.Copy(io.Discard, headResp.Body)
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
				if resp != nil {
					resp.Body.Close()
				}
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
