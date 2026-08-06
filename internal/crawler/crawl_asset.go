package crawler

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// crawlAsset downloads a static asset directly over HTTP instead of visiting
// it as a full browser page. Assets referenced by a page (images, styles,
// scripts, fonts) are normally captured while the page renders; this path
// covers assets discovered as links (e.g. <a href="image.jpg">) that the
// browser never loads, avoiding browser navigation overhead for each one.
func (c *Crawler) crawlAsset(ctx context.Context, urlStr string) {
	c.totalURLs.Add(1)

	if c.blockedPatternMatch(urlStr) {
		return
	}

	if p := c.storage.PathForURL(urlStr); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return
		}
	}

	assetCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(assetCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", cfgUserAgent(c.cfg))
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Download failures are routine during a crawl (404s, blocked URLs,
		// transient network errors); keep them at Debug to avoid flooding the
		// error log. Save failures below are real errors and log at Error.
		util.LogDebug("asset download failed", zap.String("url", urlStr), zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard response body", zap.Error(copyErr), zap.String("url", urlStr))
		}
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, config.MaxResponseBodySize+1))
	if err != nil || len(body) == 0 {
		return
	}
	if len(body) > config.MaxResponseBodySize {
		body = body[:config.MaxResponseBodySize]
	}

	localPath, err := c.storage.SaveFile(urlStr, body, resp.Header.Get("Content-Type"))
	if err != nil || localPath == "" {
		util.LogError("asset save failed", err, zap.String("url", urlStr))
		return
	}

	c.rewriter.AddMapping(urlStr, localPath)

	hashStr := strconv.FormatUint(xxhash.Sum64(body), 36)
	c.contentHashes.Put(hashStr, localPath)
	c.metrics.IncAssetsCaptured()
	c.metrics.AddBytes(int64(len(body)))
	util.LogDebug("asset downloaded directly", zap.String("url", urlStr), zap.Int("bytes", len(body)))
}
