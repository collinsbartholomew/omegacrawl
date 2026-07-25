package jsengine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

type InfiniteScrollConfig struct {
	Enabled          bool
	MaxScrolls       int
	MaxDuration      time.Duration
	StablePasses     int
	ItemSelector     string
	ScrollContainer  string
	LoadMoreSelector string
	ScrollDelay      time.Duration
	ScrollDistance    int
}

type ScrollResult struct {
	TotalItems     int
	TotalScrolls   int
	NewItemsFound  int
	Duration       time.Duration
	Reason         string
	ItemsPerScroll []int
}

func InfiniteScroll(ctx context.Context, cfg *InfiniteScrollConfig) (*ScrollResult, error) {
	if !cfg.Enabled {
		return &ScrollResult{}, nil
	}

	start := time.Now()
	result := &ScrollResult{
		ItemsPerScroll: make([]int, 0),
	}

	var prevItemCount int
	stablePasses := 0

	util.LogInfo("starting infinite scroll",
		zap.Int("max_scrolls", cfg.MaxScrolls),
		zap.Duration("max_duration", cfg.MaxDuration),
		zap.Int("stable_passes", cfg.StablePasses),
	)

	for scroll := 0; scroll < cfg.MaxScrolls; scroll++ {
		if time.Since(start) > cfg.MaxDuration {
			result.Reason = "max_duration"
			break
		}

		var currentCount int
		selJSON, _ := json.Marshal(cfg.ItemSelector)
		err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
			(function(selector) {
				const items = document.querySelectorAll(selector);
				return items.length;
			})(%s)
		`, string(selJSON)), &currentCount))
		if err != nil {
			util.LogDebug("failed to count items", zap.Error(err))
			break
		}

		result.ItemsPerScroll = append(result.ItemsPerScroll, currentCount)

		if currentCount == prevItemCount {
			stablePasses++
			if stablePasses >= cfg.StablePasses {
				result.Reason = "stable_count"
				util.LogInfo("infinite scroll complete - stable count",
					zap.Int("scrolls", scroll),
					zap.Int("items", currentCount),
				)
				break
			}
		} else {
			stablePasses = 0
		}

		prevItemCount = currentCount

		if cfg.LoadMoreSelector != "" {
			var clicked bool
			selJSON, _ := json.Marshal(cfg.LoadMoreSelector)
			err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
				(function(selector) {
					const btn = document.querySelector(selector);
					if (btn && btn.offsetParent !== null) {
						btn.click();
						return true;
					}
					return false;
				})(%s)
			`, string(selJSON)), &clicked))
			if err == nil && clicked {
				timer := time.NewTimer(cfg.ScrollDelay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					result.Reason = "ctx_cancelled"
				}
				timer.Stop()
				continue
			}
		}

		if cfg.ScrollContainer != "" {
			selJSON, _ := json.Marshal(cfg.ScrollContainer)
			err = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
				(function(container) {
					const el = document.querySelector(container);
					if (el) {
						el.scrollTo(0, el.scrollHeight);
						return true;
					}
					return false;
				})(%s)
			`, string(selJSON)), nil))
		} else {
			err = chromedp.Run(ctx, chromedp.Evaluate(`
				window.scrollTo(0, document.body.scrollHeight);
			`, nil))
		}

		if err != nil {
			util.LogDebug("scroll failed", zap.Error(err))
			break
		}

		timer := time.NewTimer(cfg.ScrollDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			result.Reason = "ctx_cancelled"
		}
		timer.Stop()
		if ctx.Err() != nil {
			break
		}
	}

	var finalCount int
	itemJSON, _ := json.Marshal(cfg.ItemSelector)
	err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
		(function(selector) {
			return document.querySelectorAll(selector).length;
		})(%s)
	`, string(itemJSON)), &finalCount))
	if err == nil {
		result.TotalItems = finalCount
	}

	result.TotalScrolls = len(result.ItemsPerScroll)
	if len(result.ItemsPerScroll) > 0 {
		result.NewItemsFound = finalCount - result.ItemsPerScroll[0]
	}
	result.Duration = time.Since(start)

	if result.Reason == "" {
		result.Reason = "max_scrolls"
	}

	util.LogInfo("infinite scroll finished",
		zap.Int("total_items", result.TotalItems),
		zap.Int("total_scrolls", result.TotalScrolls),
		zap.Duration("duration", result.Duration),
		zap.String("reason", result.Reason),
	)

	return result, nil
}
