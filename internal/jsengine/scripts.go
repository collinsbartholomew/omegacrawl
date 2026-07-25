package jsengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	StealthScript = `
		// Override webdriver detection
		Object.defineProperty(navigator, 'webdriver', {
			get: () => undefined
		});

		// Override plugins
		Object.defineProperty(navigator, 'plugins', {
			get: () => {
				const plugins = [
					{ name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer' },
					{ name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai' },
					{ name: 'Native Client', filename: 'internal-nacl-plugin' }
				];
				plugins.length = 3;
				return plugins;
			}
		});

		// Override languages
		Object.defineProperty(navigator, 'languages', {
			get: () => ['en-US', 'en']
		});

		// Override permissions
		const originalQuery = window.navigator.permissions.query;
		window.navigator.permissions.query = (parameters) => (
			parameters.name === 'notifications' ?
				Promise.resolve({ state: Notification.permission }) :
				originalQuery(parameters)
		);

		// Chrome runtime
		window.chrome = {
			runtime: {},
			loadTimes: function() {},
			csi: function() {},
			app: {}
		};

		// Override WebGL vendor and renderer
		const getParameter = WebGLRenderingContext.prototype.getParameter;
		WebGLRenderingContext.prototype.getParameter = function(parameter) {
			if (parameter === 37445) {
				return 'Intel Inc.';
			}
			if (parameter === 37446) {
				return 'Intel Iris OpenGL Engine';
			}
			return getParameter.apply(this, arguments);
		};
	`

	LazyLoadScript = `
		// Trigger lazy loading for all images
		document.querySelectorAll('img[data-src], img[data-lazy-src], img[loading="lazy"]').forEach(img => {
			if (img.dataset.src) img.src = img.dataset.src;
			if (img.dataset.lazySrc) img.src = img.dataset.lazySrc;
		});

		// Trigger lazy loading for background images
		document.querySelectorAll('[data-bg], [data-background]').forEach(el => {
			const bg = el.dataset.bg || el.dataset.background;
			if (bg) el.style.backgroundImage = 'url(' + bg + ')';
		});

		// Trigger lazy loading for iframes
		document.querySelectorAll('iframe[data-src]').forEach(iframe => {
			if (iframe.dataset.src) iframe.src = iframe.dataset.src;
		});
	`

	InfiniteScrollScript = `
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

	ScrollToBottomScript = `
		window.scrollTo(0, document.body.scrollHeight);
	`

	ClickLoadMoreScript = `
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

	DiscoverRoutesScript = `
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

	ExtractShadowDOMScript = `
		(function() {
			function deepQuery(root, selector) {
				const results = [];
				const elements = root.querySelectorAll(selector);
				elements.forEach(el => results.push(el.outerHTML));

				// Penetrate shadow DOM
				root.querySelectorAll('*').forEach(el => {
					if (el.shadowRoot) {
						results.push(...deepQuery(el.shadowRoot, selector));
					}
				});

				return results;
			}

			// Get all shadow roots
			const shadowRoots = [];
			document.querySelectorAll('*').forEach(el => {
				if (el.shadowRoot) {
					shadowRoots.push({
						tag: el.tagName.toLowerCase(),
						innerHTML: el.shadowRoot.innerHTML
					});
				}
			});

			return {
				shadowRoots: shadowRoots,
				count: shadowRoots.length
			};
		})()
	`

	DetectFrameworkScript = `
		(function() {
			const detection = {
				framework: 'unknown',
				version: null,
				isHydrated: false,
				hasSkeleton: false,
				loadingState: 'loaded'
			};

			// Check for loading indicators
			const loaders = document.querySelectorAll('.loading, .spinner, .loader, [class*="loading"], [class*="spinner"]');
			detection.hasSkeleton = loaders.length > 0;

			// Check for skeleton screens
			const skeletons = document.querySelectorAll('[class*="skeleton"], [class*="placeholder"], [class*="shimmer"]');
			detection.hasSkeleton = detection.hasSkeleton || skeletons.length > 0;

			// Next.js
			if (window.__NEXT_DATA__) {
				detection.framework = 'nextjs';
				detection.isHydrated = true;
			}

			// Nuxt
			if (window.__NUXT__) {
				detection.framework = 'nuxt';
				detection.isHydrated = true;
			}

			// Angular
			const ngVersion = document.querySelector('[ng-version]');
			if (ngVersion) {
				detection.framework = 'angular';
				detection.version = ngVersion.getAttribute('ng-version');
				detection.isHydrated = true;
			}

			// React
			if (document.querySelector('[data-reactroot]') || document.querySelector('#__next')) {
				detection.framework = 'react';
				detection.isHydrated = !!document.querySelector('[data-reactroot]');
			}

			// Vue
			if (document.querySelector('[data-v-]') || window.__VUE__) {
				detection.framework = 'vue';
				detection.isHydrated = true;
			}

			// Svelte
			if (document.querySelector('[class*="svelte-"]')) {
				detection.framework = 'svelte';
				detection.isHydrated = true;
			}

			// Check loading state
			if (document.readyState === 'loading') {
				detection.loadingState = 'loading';
			} else if (document.readyState === 'interactive') {
				detection.loadingState = 'domcontentloaded';
			} else {
				detection.loadingState = 'loaded';
			}

			return detection;
		})()
	`

	WaitForSelectorScript = `
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

	WaitForNetworkIdleScript = `
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

	PushStateCaptureScript = `
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

	GetPushStateRoutesLScript = `
		(function() {
			return JSON.stringify(window.__pushStateRoutes || []);
		})()
	`

	ExtractIframeSourcesScript = `
		(function() {
			const iframes = document.querySelectorAll('iframe, frame');
			const results = [];
			const seen = new Set();
			iframes.forEach(iframe => {
				let src = iframe.getAttribute('src');
				if (!src) {
					src = iframe.getAttribute('data-src') || iframe.getAttribute('data-lazy-src');
				}
				if (src && !seen.has(src)) {
					seen.add(src);
					results.push({
						src: src,
						width: iframe.getAttribute('width') || '',
						height: iframe.getAttribute('height') || '',
						id: iframe.id || '',
						sandbox: iframe.getAttribute('sandbox') || ''
					});
				}
			});
			return JSON.stringify(results);
		})()
	`

	ExtractMediaSourcesScript = `
		(function() {
			const results = [];
			const seen = new Set();
			function addSource(src, type, el) {
				if (src && !seen.has(src)) {
					seen.add(src);
					results.push({
						src: src,
						type: type,
						poster: el.getAttribute('poster') || '',
						width: el.getAttribute('width') || '',
						height: el.getAttribute('height') || '',
						id: el.id || ''
					});
				}
			}
			document.querySelectorAll('video, audio').forEach(media => {
				const src = media.getAttribute('src');
				if (src) addSource(src, media.tagName.toLowerCase(), media);
				media.querySelectorAll('source').forEach(source => {
					const s = source.getAttribute('src');
					if (s) addSource(s, media.tagName.toLowerCase() + '-source', source);
				});
			});
			document.querySelectorAll('source').forEach(source => {
				const parent = source.parentElement;
				if (parent && parent.tagName !== 'VIDEO' && parent.tagName !== 'AUDIO') {
					const s = source.getAttribute('src');
					if (s) addSource(s, 'source', source);
				}
			});
			return JSON.stringify(results);
		})()
	`

	ExtractStructuredDataScript = `
		(function() {
			var result = { jsonld: [], og: {}, twitter: {}, meta: {} };
			document.querySelectorAll('script[type="application/ld+json"]').forEach(function(el) {
				try {
					result.jsonld.push(JSON.parse(el.textContent));
				} catch(e) {}
			});
			document.querySelectorAll('meta[property^="og:"]').forEach(function(el) {
				var key = el.getAttribute('property').replace('og:', '');
				result.og[key] = el.getAttribute('content') || '';
			});
			document.querySelectorAll('meta[name^="twitter:"]').forEach(function(el) {
				var key = el.getAttribute('name').replace('twitter:', '');
				result.twitter[key] = el.getAttribute('content') || '';
			});
			document.querySelectorAll('meta[name], meta[property]').forEach(function(el) {
				var name = el.getAttribute('name') || el.getAttribute('property') || '';
				if (!name.startsWith('og:') && !name.startsWith('twitter:')) {
					result.meta[name] = el.getAttribute('content') || '';
				}
			});
			return JSON.stringify(result);
		})()
	`
)

type RouteInfo struct {
	Frameworks []FrameworkInfo `json:"frameworks"`
	Routes     []string        `json:"routes"`
	Title      string          `json:"title"`
	URL        string          `json:"url"`
}

type FrameworkInfo struct {
	Framework string `json:"framework"`
	Version   string `json:"version,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

type ShadowDOMInfo struct {
	ShadowRoots []ShadowRoot `json:"shadowRoots"`
	Count        int          `json:"count"`
}

type ShadowRoot struct {
	Tag        string `json:"tag"`
	InnerHTML  string `json:"innerHTML"`
}

type FrameworkDetection struct {
	Framework    string `json:"framework"`
	Version      string `json:"version,omitempty"`
	IsHydrated   bool   `json:"isHydrated"`
	HasSkeleton  bool   `json:"hasSkeleton"`
	LoadingState string `json:"loadingState"`
}

type ScrollMetrics struct {
	ScrollHeight   int `json:"scrollHeight"`
	ScrollTop      int `json:"scrollTop"`
	ClientHeight   int `json:"clientHeight"`
	ItemCount      int `json:"itemCount"`
	ViewportHeight int `json:"viewportHeight"`
}

func InjectStealth(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(StealthScript, nil))
}

func InjectLazyLoad(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(LazyLoadScript, nil))
}

func GetScrollMetrics(ctx context.Context) (*ScrollMetrics, error) {
	var result ScrollMetrics
	err := chromedp.Run(ctx, chromedp.Evaluate(InfiniteScrollScript, &result))
	return &result, err
}

func ScrollToBottom(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(ScrollToBottomScript, nil))
}

func ClickLoadMore(ctx context.Context) (bool, error) {
	var result bool
	err := chromedp.Run(ctx, chromedp.Evaluate(ClickLoadMoreScript, &result))
	return result, err
}

func ClickElement(ctx context.Context, selector string) error {
	safeSelector, _ := json.Marshal(selector)
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
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	return err
}

func DiscoverRoutes(ctx context.Context) (*RouteInfo, error) {
	var result RouteInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(DiscoverRoutesScript, &result))
	return &result, err
}

func ExtractShadowDOM(ctx context.Context) (*ShadowDOMInfo, error) {
	var result ShadowDOMInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractShadowDOMScript, &result))
	return &result, err
}

func DetectFramework(ctx context.Context) (*FrameworkDetection, error) {
	var result FrameworkDetection
	err := chromedp.Run(ctx, chromedp.Evaluate(DetectFrameworkScript, &result))
	return &result, err
}

func WaitForSelector(ctx context.Context, selector string, timeout time.Duration) (bool, error) {
	var result map[string]interface{}
	safeSelector, _ := json.Marshal(selector)
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
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false, err
	}
	if found, ok := result["found"].(bool); ok {
		return found, nil
	}
	return false, nil
}

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

type PushStateRoute struct {
	URL       string `json:"url"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"`
}

type IframeSource struct {
	Src     string `json:"src"`
	Width   string `json:"width"`
	Height  string `json:"height"`
	ID      string `json:"id"`
	Sandbox string `json:"sandbox"`
}

type MediaSource struct {
	Src    string `json:"src"`
	Type   string `json:"type"`
	Poster string `json:"poster"`
	Width  string `json:"width"`
	Height string `json:"height"`
	ID     string `json:"id"`
}

type StructuredData struct {
	JSONLD   []interface{}     `json:"jsonld"`
	OG       map[string]string `json:"og"`
	Twitter  map[string]string `json:"twitter"`
	Meta     map[string]string `json:"meta"`
}

func InjectPushStateCapture(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(PushStateCaptureScript, nil))
}

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

func ExtractIframeSources(ctx context.Context) ([]IframeSource, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractIframeSourcesScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return []IframeSource{}, nil
	}
	var iframes []IframeSource
	if err := json.Unmarshal([]byte(resultJSON), &iframes); err != nil {
		return nil, err
	}
	return iframes, nil
}

func ExtractMediaSources(ctx context.Context) ([]MediaSource, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractMediaSourcesScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return []MediaSource{}, nil
	}
	var media []MediaSource
	if err := json.Unmarshal([]byte(resultJSON), &media); err != nil {
		return nil, err
	}
	return media, nil
}

func ExtractStructuredData(ctx context.Context) (*StructuredData, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractStructuredDataScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return &StructuredData{}, nil
	}
	var data StructuredData
	if err := json.Unmarshal([]byte(resultJSON), &data); err != nil {
		return nil, err
	}
	if data.OG == nil {
		data.OG = make(map[string]string)
	}
	if data.Twitter == nil {
		data.Twitter = make(map[string]string)
	}
	if data.Meta == nil {
		data.Meta = make(map[string]string)
	}
	return &data, nil
}

func ScrollToElement(ctx context.Context, selector string) error {
	safeSelector, _ := json.Marshal(selector)
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

type ArticleContent struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	URL         string `json:"url"`
	ExtractedAt string `json:"extracted_at"`
}

func ExtractArticle(ctx context.Context) (*ArticleContent, error) {
	var result ArticleContent
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractArticleScript, &result))
	return &result, err
}

func GenerateSingleFile(ctx context.Context) (string, error) {
	var result string
	err := chromedp.Run(ctx, chromedp.Evaluate(SingleFileScript, &result))
	return result, err
}

const (
	ExtractArticleScript = `
		(function() {
			function getArticleContent() {
				// Try readability-style extraction
				const article = document.querySelector('article');
				if (article) return article.outerHTML;

				// Try common content selectors
				const selectors = [
					'[role="main"]',
					'.main-content',
					'.content',
					'#content',
					'.post-content',
					'.entry-content',
					'.article-body',
					'[itemprop="articleBody"]'
				];
				for (const sel of selectors) {
					const el = document.querySelector(sel);
					if (el) return el.outerHTML;
				}

				// Fallback: largest text container
				const candidates = document.querySelectorAll('div, section, main');
				let best = null;
				let maxText = 0;
				candidates.forEach(el => {
					const text = el.innerText || '';
					if (text.length > maxText) {
						maxText = text.length;
						best = el;
					}
				});
				return best ? best.outerHTML : document.body.innerHTML;
			}

			return {
				title: document.title,
				content: getArticleContent(),
				url: window.location.href,
				extractedAt: new Date().toISOString()
			};
		})()
	`

	SingleFileScript = `
		(function() {
			function inlineResources() {
				// Inline CSS
				const styles = document.querySelectorAll('link[rel="stylesheet"]');
				styles.forEach(link => {
					const href = link.href;
					fetch(href).then(r => r.text()).then(css => {
						const style = document.createElement('style');
						style.textContent = css;
						link.parentNode.replaceChild(style, link);
					}).catch(() => {});
				});

				// Inline scripts
				const scripts = document.querySelectorAll('script[src]');
				scripts.forEach(script => {
					const src = script.src;
					fetch(src).then(r => r.text()).then(js => {
						const inline = document.createElement('script');
						inline.textContent = js;
						script.parentNode.replaceChild(inline, script);
					}).catch(() => {});
				});

				// Inline images as data URLs (optional, can be large)
				const images = document.querySelectorAll('img[src]');
				images.forEach(img => {
					const src = img.src;
					if (src.startsWith('data:')) return;
					fetch(src).then(r => r.blob()).then(blob => {
						const reader = new FileReader();
						reader.onloadend = () => {
							img.src = reader.result;
						};
						reader.readAsDataURL(blob);
					}).catch(() => {});
				});
			}

			inlineResources();
			return document.documentElement.outerHTML;
		})()
	`
)
