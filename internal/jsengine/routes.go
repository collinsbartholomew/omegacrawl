package jsengine

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const DiscoverRoutesScript = `
		(function() {
			// Check for common SPA frameworks
			const routes = [];

			// React Router
			if (window.__NEXT_DATA__) {
				routes.push({ framework: 'nextjs', data: window.__NEXT_DATA__ });
			}

			// Nuxt
			if (window.__NUXT__) {
				routes.push({ framework: 'nuxt', data: window.__NUXT__ });
			}

			// Angular
			const ngVersion = document.querySelector('[ng-version]');
			if (ngVersion) {
				routes.push({ framework: 'angular', version: ngVersion.getAttribute('ng-version') });
			}

			// Vue
			if (document.querySelector('[data-v-]') || window.__VUE__) {
				routes.push({ framework: 'vue' });
			}

			// Svelte
			if (document.querySelector('[class*="svelte-"]')) {
				routes.push({ framework: 'svelte' });
			}

			// Extract all links for route discovery
			const links = document.querySelectorAll('a[href]');
			const discoveredRoutes = [];
			links.forEach(link => {
				const href = link.getAttribute('href');
				if (href && !href.startsWith('http') && !href.startsWith('mailto:') && !href.startsWith('javascript:')) {
					discoveredRoutes.push(href);
				}
			});

			return {
				frameworks: routes,
				routes: discoveredRoutes,
				title: document.title,
				url: window.location.href
			};
		})()
	`

const PushStateCaptureScript = `
		(function() {
			if (window.__pushStateCaptured) return;
			window.__pushStateCaptured = true;
			window.__pushStateRoutes = [];
			window.__pushStateDone = false;

			const origPushState = history.pushState.bind(history);
			const origReplaceState = history.replaceState.bind(history);

			history.pushState = function(state, title, url) {
				if (url) {
					window.__pushStateRoutes.push({
						url: url,
						type: 'pushState',
						title: title || '',
						timestamp: Date.now()
					});
				}
				return origPushState.apply(this, arguments);
			};

			history.replaceState = function(state, title, url) {
				if (url) {
					window.__pushStateRoutes.push({
						url: url,
						type: 'replaceState',
						title: title || '',
						timestamp: Date.now()
					});
				}
				return origReplaceState.apply(this, arguments);
			};

			window.addEventListener('hashchange', function(e) {
				window.__pushStateRoutes.push({
					url: e.newURL,
					type: 'hashchange',
					title: document.title,
					timestamp: Date.now()
				});
			});

			window.addEventListener('popstate', function(e) {
				window.__pushStateRoutes.push({
					url: window.location.href,
					type: 'popstate',
					title: document.title,
					timestamp: Date.now()
				});
			});
		})()
	`

const GetPushStateRoutesLScript = `
		(function() {
			return JSON.stringify(window.__pushStateRoutes || []);
		})()
	`

// RouteInfo holds the frameworks and candidate routes discovered on a page.
type RouteInfo struct {
	Frameworks []FrameworkInfo `json:"frameworks"`
	Routes     []string        `json:"routes"`
	Title      string          `json:"title"`
	URL        string          `json:"url"`
}

// PushStateRoute records a single client-side navigation event.
type PushStateRoute struct {
	URL       string `json:"url"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"`
}

// DiscoverRoutes detects SPA frameworks and extracts candidate client-side routes.
func DiscoverRoutes(ctx context.Context) (*RouteInfo, error) {
	var result RouteInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(DiscoverRoutesScript, &result))
	return &result, err
}

// InjectPushStateCapture instruments history APIs to record client-side navigation.
func InjectPushStateCapture(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(PushStateCaptureScript, nil))
}

// GetPushStateRoutes returns the routes captured by InjectPushStateCapture.
func GetPushStateRoutes(ctx context.Context) ([]PushStateRoute, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(GetPushStateRoutesLScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return []PushStateRoute{}, nil
	}
	var routes []PushStateRoute
	if err := json.Unmarshal([]byte(resultJSON), &routes); err != nil {
		return nil, err
	}
	return routes, nil
}
