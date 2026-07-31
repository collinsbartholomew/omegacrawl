package jsengine

import (
	"context"
	"encoding/json"
	"fmt"
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
	var result map[string]interface{}
	err = chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	return err
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
