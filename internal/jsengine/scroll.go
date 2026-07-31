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

// InfiniteScrollConfig controls the behaviour of infinite scroll extraction.
type InfiniteScrollConfig struct {
	Enabled          bool
	MaxScrolls       int
	MaxDuration      time.Duration
	StablePasses     int
	ItemSelector     string
	ScrollContainer  string
	LoadMoreSelector string
	ScrollDelay      time.Duration
	ScrollDistance   int
}

// ScrollResult summarizes the outcome of an infinite scroll run.
type ScrollResult struct {
	TotalItems     int
	TotalScrolls   int
	NewItemsFound  int
	Duration       time.Duration
	Reason         string
	ItemsPerScroll []int
}

// InfiniteScroll repeatedly scrolls the page and clicks load-more buttons until items stabilize or limits are reached.
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
		if ctx.Err() != nil {
			result.Reason = "ctx_cancelled"
			break
		}

		var currentCount int
		selJSON, err := json.Marshal(cfg.ItemSelector)
		if err != nil {
			util.LogError("failed to marshal item selector", err)
			break
		}
		err = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
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
			selJSON, err := json.Marshal(cfg.LoadMoreSelector)
			if err != nil {
				util.LogError("failed to marshal load-more selector", err)
				continue
			}
			err = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
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
					if !timer.Stop() {
						<-timer.C
					}
					result.Reason = "ctx_cancelled"
				}
				continue
			}
		}

		if cfg.ScrollContainer != "" {
			selJSON, err := json.Marshal(cfg.ScrollContainer)
			if err != nil {
				util.LogError("failed to marshal scroll container selector", err)
				break
			}
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
			if !timer.Stop() {
				<-timer.C
			}
			result.Reason = "ctx_cancelled"
		}
		if ctx.Err() != nil {
			break
		}
	}

	var finalCount int
	itemJSON, err := json.Marshal(cfg.ItemSelector)
	if err != nil {
		util.LogError("failed to marshal item selector", err)
	} else {
		err = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
			(function(selector) {
				return document.querySelectorAll(selector).length;
			})(%s)
		`, string(itemJSON)), &finalCount))
		if err == nil {
			result.TotalItems = finalCount
		}
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
