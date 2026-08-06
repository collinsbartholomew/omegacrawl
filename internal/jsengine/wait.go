package jsengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"
)

// WaitStrategy defines a strategy for waiting on page conditions.
type WaitStrategy interface {
	Wait(ctx context.Context) error
	Name() string
}

// WaitForResponseStrategy waits until a resource matching the URL pattern is loaded or the timeout elapses.
type WaitForResponseStrategy struct {
	URLPattern string
	Timeout    time.Duration
}

// Wait waits until a network resource matching the URL pattern is observed or the timeout elapses.
func (w *WaitForResponseStrategy) Wait(ctx context.Context) error {
	patternJSON, err := json.Marshal(w.URLPattern)
	if err != nil {
		return fmt.Errorf("failed to marshal url pattern: %w", err)
	}
	script := fmt.Sprintf(`
		(function(pattern, timeout) {
			return new Promise((resolve) => {
				const startTime = Date.now();
				const observer = new PerformanceObserver((list) => {
					for (const entry of list.getEntries()) {
						if (entry.name.includes(pattern)) {
							observer.disconnect();
							resolve({ found: true, url: entry.name });
							return;
						}
					}
				});

				try {
					observer.observe({ entryTypes: ['resource'] });
				} catch(e) {}

				setTimeout(() => {
					observer.disconnect();
					resolve({ found: false });
				}, timeout);
			});
		})(%s, %d)
	`, string(patternJSON), w.Timeout.Milliseconds())
	var result struct {
		Found bool   `json:"found"`
		URL   string `json:"url"`
	}
	if err = chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		return err
	}
	if !result.Found {
		return fmt.Errorf("wait for response pattern %q timed out", w.URLPattern)
	}
	return nil
}

// Name returns the strategy name "response".
func (w *WaitForResponseStrategy) Name() string { return "response" }

// AdaptiveWaitStrategy waits for framework-specific readiness signals based on the detected framework.
type AdaptiveWaitStrategy struct {
	Framework    string
	SelectorWait time.Duration
	NetworkWait  time.Duration
	MaxWait      time.Duration
}

// Wait waits for the page to become ready using the strategy matching the configured framework.
func (a *AdaptiveWaitStrategy) Wait(ctx context.Context) error {
	switch a.Framework {
	case "react", "nextjs":
		return a.waitReact(ctx)
	case "vue", "nuxt":
		return a.waitVue(ctx)
	case "angular":
		return a.waitAngular(ctx)
	case "svelte":
		return a.waitSvelte(ctx)
	default:
		return a.waitGeneric(ctx)
	}
}

func (a *AdaptiveWaitStrategy) waitReact(ctx context.Context) error {
	script := fmt.Sprintf(`
		(function(timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					const root = document.querySelector('#__next, #root, [data-reactroot]');
					if (root && root.children.length > 0) {
						resolve({ ready: true, framework: 'react' });
					} else if (Date.now() - start > timeout) {
						resolve({ ready: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})(%d)
	`, a.MaxWait.Milliseconds())
	var result map[string]interface{}
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result))
}

func (a *AdaptiveWaitStrategy) waitVue(ctx context.Context) error {
	script := fmt.Sprintf(`
		(function(timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					const app = document.querySelector('#app, #__nuxt, [data-v-app]');
					if (app && app.children.length > 0) {
						resolve({ ready: true, framework: 'vue' });
					} else if (Date.now() - start > timeout) {
						resolve({ ready: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})(%d)
	`, a.MaxWait.Milliseconds())
	var result map[string]interface{}
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result))
}

func (a *AdaptiveWaitStrategy) waitAngular(ctx context.Context) error {
	script := fmt.Sprintf(`
		(function(timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					const app = document.querySelector('[ng-version], app-root');
					if (app && app.children.length > 0) {
						resolve({ ready: true, framework: 'angular' });
					} else if (Date.now() - start > timeout) {
						resolve({ ready: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})(%d)
	`, a.MaxWait.Milliseconds())
	var result map[string]interface{}
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result))
}

func (a *AdaptiveWaitStrategy) waitSvelte(ctx context.Context) error {
	return a.waitGeneric(ctx)
}

func (a *AdaptiveWaitStrategy) waitGeneric(ctx context.Context) error {
	script := fmt.Sprintf(`
		(function(timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					if (document.readyState === 'complete') {
						resolve({ ready: true, framework: 'generic' });
					} else if (Date.now() - start > timeout) {
						resolve({ ready: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})(%d)
	`, a.MaxWait.Milliseconds())
	var result map[string]interface{}
	return chromedp.Run(ctx, chromedp.Evaluate(script, &result))
}

// Name returns the strategy name "adaptive".
func (a *AdaptiveWaitStrategy) Name() string { return "adaptive" }

const WaitForSelectorScript = `
		(function(selector, timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					const el = document.querySelector(selector);
					if (el) {
						resolve({ found: true, html: el.outerHTML.substring(0, 500) });
					} else if (Date.now() - start > timeout) {
						resolve({ found: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})('%s', %d)
	`

const WaitForNetworkIdleScript = `
		(function(quietPeriod) {
			return new Promise((resolve) => {
				let lastActivity = Date.now();
				let pending = 0;

				const observer = new PerformanceObserver((list) => {
					for (const entry of list.getEntries()) {
						if (entry.initiatorType === 'fetch' || entry.initiatorType === 'xmlhttprequest') {
							lastActivity = Date.now();
						}
					}
				});

				try {
					observer.observe({ entryTypes: ['resource'] });
				} catch(e) {}

				const check = () => {
					if (Date.now() - lastActivity > quietPeriod) {
						observer.disconnect();
						resolve({ idle: true, waitTime: Date.now() - lastActivity });
					} else {
						setTimeout(check, 100);
					}
				};

				setTimeout(check, quietPeriod);
			});
		})(%d)
	`

// WaitForSelector polls until an element matching the selector appears or the timeout elapses.
func WaitForSelector(ctx context.Context, selector string, timeout time.Duration) (bool, error) {
	var result map[string]interface{}
	safeSelector, err := json.Marshal(selector)
	if err != nil {
		return false, fmt.Errorf("failed to marshal selector: %w", err)
	}
	script := `
		(function(selector, timeout) {
			return new Promise((resolve) => {
				const start = Date.now();
				const check = () => {
					const el = document.querySelector(selector);
					if (el) {
						resolve({ found: true, html: el.outerHTML.substring(0, 500) });
					} else if (Date.now() - start > timeout) {
						resolve({ found: false });
					} else {
						setTimeout(check, 100);
					}
				};
				check();
			});
		})(` + string(safeSelector) + `, ` + strconv.FormatInt(timeout.Milliseconds(), 10) + `)
	`
	err = chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false, err
	}
	if found, ok := result["found"].(bool); ok {
		return found, nil
	}
	return false, nil
}

// WaitForNetworkIdle waits until no fetch or XHR activity occurs for the given quiet period.
func WaitForNetworkIdle(ctx context.Context, quietPeriod time.Duration) (bool, error) {
	var result map[string]interface{}
	script := fmt.Sprintf(`
		(function(quietPeriod) {
			return new Promise((resolve) => {
				let lastActivity = Date.now();
				const observer = new PerformanceObserver((list) => {
					for (const entry of list.getEntries()) {
						if (entry.initiatorType === 'fetch' || entry.initiatorType === 'xmlhttprequest') {
							lastActivity = Date.now();
						}
					}
				});
				try { observer.observe({ entryTypes: ['resource'] }); } catch(e) {}
				const check = () => {
					if (Date.now() - lastActivity > quietPeriod) {
						observer.disconnect();
						resolve({ idle: true });
					} else {
						setTimeout(check, 100);
					}
				};
				setTimeout(check, quietPeriod);
			});
		})(%d)
	`, quietPeriod.Milliseconds())
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false, err
	}
	if idle, ok := result["idle"].(bool); ok {
		return idle, nil
	}
	return false, nil
}

// ClickElement clicks the element matching the given selector, if present.
func ClickElement(ctx context.Context, selector string) error {
	safeSelector, err := json.Marshal(selector)
	if err != nil {
		return fmt.Errorf("failed to marshal selector: %w", err)
	}
	script := fmt.Sprintf(`
		(function(selector) {
			const el = document.querySelector(selector);
			if (el) {
				el.click();
				return true;
			}
			return false;
		})(%s)
	`, string(safeSelector))
	var result bool
	err = chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	return err
}
