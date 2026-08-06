package crawler

import (
	"context"
	"time"

	crawlerrors "github.com/user/clone/internal/errors"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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

		tabCtx, err := c.browserPool.Acquire()
		if err != nil {
			util.LogError("failed to acquire browser from pool, skipping", err, zap.String("url", urlStr))
			return
		}

		lastErr = c.doCrawl(tabCtx, urlStr, depth)
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
