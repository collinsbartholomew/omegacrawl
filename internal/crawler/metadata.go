package crawler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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
					if _, copyErr := io.Copy(io.Discard, headResp.Body); copyErr != nil {
						util.LogDebug("failed to discard head response body", zap.Error(copyErr), zap.String("url", f.url))
					}
					headResp.Body.Close()
				}
				return
			}
			if _, copyErr := io.Copy(io.Discard, headResp.Body); copyErr != nil {
				util.LogDebug("failed to discard head response body", zap.Error(copyErr), zap.String("url", f.url))
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
