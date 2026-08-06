package crawler

import (
	"context"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/jsengine"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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
		waitTimer := time.NewTimer(c.cfg.WaitStrategyTimeout)
		select {
		case <-waitTimer.C:
		case <-ctx.Done():
			if !waitTimer.Stop() {
				<-waitTimer.C
			}
		}
	}
}
