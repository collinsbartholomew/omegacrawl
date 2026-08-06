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

const InfiniteScrollScript = `
		// Return current scroll metrics
		(function() {
			return {
				scrollHeight: document.body.scrollHeight,
				scrollTop: window.scrollY || document.documentElement.scrollTop,
				clientHeight: document.documentElement.clientHeight,
				itemCount: document.querySelectorAll('[data-infinite-scroll-item], .feed-item, .list-item, .card, article').length,
				viewportHeight: window.innerHeight
			};
		})()
	`

const ScrollToBottomScript = `
		window.scrollTo(0, document.body.scrollHeight);
	`

const ClickLoadMoreScript = `
		(function() {
			const selectors = [
				'button.load-more',
				'button[data-action="load-more"]',
				'.load-more-button',
				'.load-more',
				'[data-testid="load-more"]',
				'button:has-text("Load More")',
				'a.load-more',
				'.show-more',
				'button.show-more',
				'[data-action="show-more"]'
			];
			for (const sel of selectors) {
				try {
					const btn = document.querySelector(sel);
					if (btn && btn.offsetParent !== null) {
						btn.click();
						return true;
					}
				} catch(e) {}
			}
			// Try text-based matching
			const buttons = document.querySelectorAll('button, a, [role="button"]');
			for (const btn of buttons) {
				const text = btn.textContent.toLowerCase().trim();
				if (text === 'load more' || text === 'show more' || text === 'view more' || text === 'see more') {
					if (btn.offsetParent !== null) {
						btn.click();
						return true;
					}
				}
			}
			return false;
		})()
	`

// ScrollMetrics captures the current scroll and content metrics of the page.
type ScrollMetrics struct {
	ScrollHeight   int `json:"scrollHeight"`
	ScrollTop      int `json:"scrollTop"`
	ClientHeight   int `json:"clientHeight"`
	ItemCount      int `json:"itemCount"`
	ViewportHeight int `json:"viewportHeight"`
}

// GetScrollMetrics returns the current scroll and item metrics of the page.
func GetScrollMetrics(ctx context.Context) (*ScrollMetrics, error) {
	var result ScrollMetrics
	err := chromedp.Run(ctx, chromedp.Evaluate(InfiniteScrollScript, &result))
	return &result, err
}

// ScrollToBottom scrolls the window to the bottom of the page.
func ScrollToBottom(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(ScrollToBottomScript, nil))
}

// ClickLoadMore clicks a visible load-more button and reports whether one was found.
func ClickLoadMore(ctx context.Context) (bool, error) {
	var result bool
	err := chromedp.Run(ctx, chromedp.Evaluate(ClickLoadMoreScript, &result))
	return result, err
}

// ScrollToElement scrolls the element matching the selector into view.
func ScrollToElement(ctx context.Context, selector string) error {
	safeSelector, err := json.Marshal(selector)
	if err != nil {
		return fmt.Errorf("failed to marshal selector: %w", err)
	}
	script := `
		(function(selector) {
			const el = document.querySelector(selector);
			if (el) {
				el.scrollIntoView({ behavior: 'smooth', block: 'center' });
				return true;
			}
			return false;
		})(` + string(safeSelector) + `)
	`
	var result bool
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result))
}

// ExpandAllSections expands collapsible, accordion, and details elements on the page.
func ExpandAllSections(ctx context.Context) error {
	script := `
		// Click all expandable elements
		document.querySelectorAll('[data-toggle], [aria-expanded="false"], details:not([open]), .collapsible:not(.active)').forEach(el => {
			try { el.click(); } catch(e) {}
		});

		// Expand all details elements
		document.querySelectorAll('details').forEach(d => d.open = true);

		// Click all accordion headers
		document.querySelectorAll('.accordion-header, .accordion-trigger, [role="button"][aria-expanded="false"]').forEach(el => {
			try { el.click(); } catch(e) {}
		});
	`
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}

// DismissOverlays dismisses cookie consents, modals, and fixed overlay elements.
func DismissOverlays(ctx context.Context) error {
	script := `
		// Dismiss cookie consent
		const cookieSelectors = [
			'[data-testid="cookie-accept"]',
			'.cookie-accept',
			'.accept-cookies',
			'#accept-cookies',
			'button[data-cookiefirst-action="accept"]',
			'.cc-dismiss',
			'#onetrust-accept-btn-handler'
		];
		cookieSelectors.forEach(sel => {
			try {
				const btn = document.querySelector(sel);
				if (btn) btn.click();
			} catch(e) {}
		});

		// Dismiss modals
		const modalSelectors = [
			'.modal-close',
			'[data-dismiss="modal"]',
			'[aria-label="Close"]',
			'.close-button',
			'button.close'
		];
		modalSelectors.forEach(sel => {
			try {
				const btn = document.querySelector(sel);
				if (btn) btn.click();
			} catch(e) {}
		});

		// Remove fixed overlays
		document.querySelectorAll('[style*="position: fixed"], [style*="position:fixed"]').forEach(el => {
			const style = window.getComputedStyle(el);
			if (style.zIndex > 1000 || el.classList.toString().includes('modal') || el.classList.toString().includes('overlay')) {
				el.remove();
			}
		});
	`
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}
