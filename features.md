# Project Feature Catalog & Implementation Plan

> **Project:** Go-based browser-driven web cloner (chromedp)
> **Binary size:** ~37MB | **Language:** Go 1.25
> **Last updated:** 2026-07-30

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Browser / Chromium Features](#2-browser--chromium-features)
3. [Tab & Page Management](#3-tab--page-management)
4. [Stealth & Anti-Detection](#4-stealth--anti-detection)
5. [Network Interception](#5-network-interception)
6. [Page Capture Pipeline](#6-page-capture-pipeline)
7. [Navigation & Waiting](#7-navigation--waiting)
8. [Interaction Engine](#8-interaction-engine)
9. [Infinite Scroll](#9-infinite-scroll)
10. [SPA Route Discovery](#10-spa-route-discovery)
11. [Authentication](#11-authentication)
12. [CAPTCHA Handling](#12-captcha-handling)
13. [Content Extraction](#13-content-extraction)
14. [Storage & Output](#14-storage--output)
15. [Crawling Infrastructure](#15-crawling-infrastructure)
16. [CLI & Configuration](#16-cli--configuration)
17. [Backend Queue Integrations](#17-backend-queue-integrations)
18. [Third-Party Integrations](#18-third-party-integrations)
19. [Bugs & Issues](#19-bugs--issues)
20. [Missing Features](#20-missing-features)
21. [Phase Plan](#21-phase-plan)

---

## 1. Architecture Overview

### Core Design

The project uses a **single-process browser model** through `chromedp` (Go CDP client). One Chrome process runs for the entire crawl, and every page gets a new CDP tab context within that process.

**Key files:**
- `cmd/clone/main.go` — CLI entry point, cobra commands
- `internal/crawler/crawler.go` — Core crawler logic (~3700 lines), all browser interactions
- `internal/config/config.go` — Configuration struct and validation
- `internal/network/interceptor.go` — CDP network interception (~606 lines)
- `internal/jsengine/scripts.go` — All JS scripts injected into pages (~1071 lines)
- `internal/jsengine/scroll.go` — Infinite scroll logic
- `internal/jsengine/wait.go` — Wait strategies
- `internal/jsengine/serviceworker.go` — Service worker detection/unregistration
- `internal/jsengine/websocket.go` — WebSocket capture helpers
- `internal/jsengine/intercept.go` — JSON extraction from page
- `internal/jsanalyzer/analyzer.go` — JS dependency URL extraction patterns
- `internal/storage/filesystem.go` — Filesystem output writer
- `internal/storage/warc.go` — WARC archive writer
- `internal/auth/auth.go` — Authentication manager
- `internal/captcha/solver.go` — CAPTCHA solving client
- `internal/robots/robots.go` — robots.txt parser
- `internal/rewrite/html.go` — HTML/CSS URL rewriter
- `internal/queue/` — Queue backends (local, redis, postgres, kafka)

### Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/chromedp/chromedp` | Chrome DevTools Protocol client |
| `github.com/chromedp/cdproto` | CDP domain types |
| `github.com/spf13/cobra` | CLI framework |
| `go.uber.org/zap` | Structured logging |
| `golang.org/x/net/html` | HTML tokenizer for link extraction |
| `golang.org/x/sync/errgroup` | Goroutine error propagation |
| `golang.org/x/sync/singleflight` | Duplicate request suppression |
| `github.com/cespare/xxhash/v2` | Content fingerprint hashing |

---

## 2. Browser / Chromium Features

All browser-related features in `internal/crawler/crawler.go` and supporting files.

### Chrome Process Management

| Feature | Status | File:Line | Implementation |
|---|---|---|---|---|
| Chrome process spawn | ✅ | `crawler.go:1392-1414` | `chromedp.NewExecAllocator` with Chrome flags |
| Headless toggle | ✅ | `crawler.go:374` | `chromedp.Flag("headless", !cfg.Interactive)` |
| Proxy via Chrome | ✅ | `crawler.go:395-397` | `chromedp.ProxyServer(proxy)` from config |
| Browser restart (legacy) | ✅ | `crawler.go:1476-1498` | Cancel old allocator, create new Chrome (replaced by pool) |
| Health check / restart | ✅ | `browserpool/pool.go:167-192` | Pool's `HealthCheck()` replaces per-page `browserCtx.Err()` |
| Multi-browser process pool | ✅ | `browserpool/pool.go:1-193` | N Chrome processes, LRU selection, auto-restart on failure |
| Configurable Chrome flags | ✅ | `config.go:84`, `crawler.go:402-417` | `ChromeFlags []string` + `--chrome-flag` CLI, appended to hardcoded set |
| Remote Chrome connection | ✅ | `config.go:86`, `browserpool/pool.go:68-73` | `RemoteChromeURL` → `chromedp.NewRemoteAllocator`, pool-integrated |

**Chrome flags set (hardcoded, `crawler.go:371-398`):**

| Flag | Value | Purpose |
|---|---|---|
| `headless` | `!Interactive` | Headless mode toggle |
| `disable-gpu` | `true` | GPU-less rendering |
| `no-sandbox` | `true` | Required in containers/CI |
| `disable-dev-shm-usage` | `true` | Avoid /dev/shm issues |
| `disable-background-networking` | `true` | Reduce network noise |
| `disable-default-apps` | `true` | Disable Chrome apps |
| `disable-extensions` | `true` | No extensions |
| `disable-sync` | `true` | No sync |
| `no-first-run` | `true` | Skip first-run dialog |
| `window-size` | configurable (1920x1080) | Viewport dimensions |
| `disable-features` | `TranslateUI,ChromeWhatsNewUI` | Disable translate/WhatsNew |
| `disable-component-update` | `true` | No component updates |
| `disable-blink-features` | `AutomationControlled` (stealth only) | Hide automation |
| `excludeSwitches` | `enable-automation` (stealth only) | No automation banner |
| `disable-renderer-backgrounding` | `true` (stealth only) | Keep rendering active |
| `start-maximized` | `true` (interactive only) | Maximize visible window |

### Issues

| Issue | Severity | Status | Details |
|---|---|---|---|
| **No multi-browser pool** | 🔴 Critical | ✅ **FIXED** | `internal/browserpool/` — N Chrome processes with health checks & auto-restart |
| **Browser restart deadlock** | 🔴 Critical | ✅ **FIXED** | Pool uses separate goroutine for restarts; old restart code uses 30s timeout context |
| **No graceful Chrome shutdown** | 🟡 Medium | ✅ **FIXED** | `browserCancel` now waits up to 5s for `allocCtx.Done()`; pool also waits on close |
| **No user data directory** | 🟡 Medium | 🔴 Open | Every restart = fresh Chrome profile. Lost sessions, localStorage, IndexedDB. See [#16] |
| **No configurable Chrome flags** | 🟡 Medium | ✅ **FIXED** | `ChromeFlags []string` + `--chrome-flag` CLI flag; appended to hardcoded flag set |
| **No remote Chrome support** | 🟡 Medium | ✅ **FIXED** | `RemoteChromeURL` → `chromedp.NewRemoteAllocator` via pool |
| **Chrome process leak** | 🟢 Low | 🟡 Open | No explicit kill on context cancel — `allocCancel()` only cancels context, doesn't SIGKILL |

---

## 3. Tab & Page Management

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Per-page CDP tab context | ✅ | `crawler.go:1507` | `chromedp.NewContext(browserCtx)` per page |
| Page timeout | ✅ | `crawler.go:1510` | `context.WithTimeout(rawTabCtx, PageTimeout)` (default 120s) |
| Max concurrent pages | ✅ | `crawler.go:209-213` | Semaphore-based goroutine limiter (default: 5) |
| Tab cleanup on return | ✅ | `crawler.go:1508` | `defer tabCancel()` |
| Console error capture | ✅ | `crawler.go:3190-3215` | `ListenTarget` for `EventConsoleAPICalled`, `EventExceptionThrown` |
| WebSocket capture | ✅ | `crawler.go:3217-3240+` | `ListenTarget` for 4 WS events |
| Auth persistent tab | ✅ | `auth.go:74-76` | `sync.Once` tab for form login |
| Memory budget | ✅ | `crawler.go:315` | `MemoryBudget` with cond-based blocking allocation |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **No tab pool** | 🟡 Medium | New CDP context per page. Context creation has overhead. Could reuse. |
| **No context deadlines on all CDP calls** | 🟡 Medium | Multiple `chromedp.Run(tabCtx, ...)` calls don't have explicit timeouts. See [Issue #9] |
| **Empty error handling on os.WriteFile** | 🟢 Low | Lines 1589-1591, 1808-1809, 1827-1828, 1836-1837 silently swallow errors. See [Issue #10] |

---

## 4. Stealth & Anti-Detection

All in `internal/jsengine/scripts.go:14-172` and `crawler.go:385-391`.

### JavaScript Overrides (StealthScript)

| Override | Status | Target | Implementation |
|---|---|---|---|
| CDP property cleanup | ✅ | `window.$cdc_*`, `window.$chrome_*` | Deleted from window |
| navigator.webdriver | ✅ | `navigator.webdriver` | `Object.defineProperty` → `undefined` |
| navigator.plugins | ✅ | `navigator.plugins` | Mock with 3 plugins (PDF, NaCl) |
| navigator.languages | ✅ | `navigator.languages` | `['en-US', 'en']` |
| permissions.query | ✅ | `navigator.permissions.query` | Notifications → `prompt` |
| chrome.runtime | ✅ | `chrome.runtime` | Full mock (id, events, APIs) |
| chrome.loadTimes | ✅ | `chrome.loadTimes` | Realistic return object |
| chrome.csi | ✅ | `chrome.csi` | Realistic return object |
| chrome.app | ✅ | `chrome.app` | isInstalled, installState, runningState |
| chrome.webstore | ✅ | `chrome.webstore` | onInstallStageChanged, onDownloadProgress |
| WebGL vendor spoof | ✅ | `WebGLRenderingContext.getParameter` | Intel Inc. / Intel Iris OpenGL Engine |
| hardwareConcurrency | ✅ | `navigator.hardwareConcurrency` | 8 |
| deviceMemory | ✅ | `navigator.deviceMemory` | 8 |
| maxTouchPoints | ✅ | `navigator.maxTouchPoints` | 0 (desktop) |
| connection.effectiveType | ✅ | `navigator.connection.effectiveType` | '4g' |
| Screen dimensions | ✅ | `screen.width/height/avail*` | 1920x1080, 24-bit color |
| speechSynthesis | ✅ | `window.speechSynthesis.speaking` | `false` |
| AudioContext | ✅ | `AudioContext.prototype.createOscillator` | Normalized sine wave |

### Chrome Flag Overrides (Stealth Mode Only)

| Flag | Value |
|---|---|
| `disable-blink-features` | `AutomationControlled` |
| `excludeSwitches` | `enable-automation` |
| `disable-renderer-backgrounding` | `true` |

### Service Worker Handling

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| SW detection | ✅ | `jsengine/websocket.go:19-39` | `navigator.serviceWorker.getRegistrations()` |
| SW unregistration | ✅ | `jsengine/websocket.go:41-57` | `reg.unregister()` for each found SW |
| SW register override | ✅ | `crawler.go:1814-1818` | Stealth mode only — injects override before navigation |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **No canvas fingerprint noise** | 🟡 Medium | Canvas rendering can detect headless by comparing image data |
| **No font fingerprint mitigation** | 🟡 Medium | Font availability list is fingerprintable |
| **No WebRTC IP leak prevention** | 🟡 Medium | `webRTC IP handling policy` not set to `disable_non_proxied_udp` |
| **No navigator.platform override** | 🟢 Low | Stays as reported by OS |
| **No navigator.vendor override** | 🟢 Low | Stays "Google Inc." |
| **No UA via CDP Emulation** | 🟢 Low | UA is set at config level, not via `Emulation.setUserAgentOverride` |

---

## 5. Network Interception

All in `internal/network/interceptor.go` (606 lines).

### CDP Event Capture

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Request capture | ✅ | `interceptor.go:147-168` | `EventRequestWillBeSent` — URL, method, POST body, headers |
| Response capture | ✅ | `interceptor.go:170-203` | `EventResponseReceived` — status code, MIME type, headers |
| Body fetch | ✅ | `interceptor.go:206-226` | `EventLoadingFinished` triggers `network.GetResponseBody()` |
| Retry on body fetch | ✅ | `interceptor.go:278-305` | Up to 3 retries with jitter |
| Worker pool | ✅ | `interceptor.go:90-103` | Configurable worker count (default: 10 or maxConcurrentPages*2) |
| Body size limit | ✅ | `interceptor.go:235-237` | `MaxResponseBodySize = 50MB` |
| URL dedup | ✅ | `interceptor.go:28` | `maxSeenURLs = 200,000` LRU |
| Resource limit | ✅ | `interceptor.go:27` | `maxResources = 50,000` |
| API response limit | ✅ | `interceptor.go:28` | `maxAPIResponses = 10,000` |

### API Detection

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| JSON content type detection | ✅ | `interceptor.go:543-552` | `isJSONContentType()` — match against known JSON MIME types |
| URL-based API detection | ✅ | `interceptor.go:554-577` | `isAPIContentType()` — match URL for api/graphql patterns |
| GraphQL op extraction | ✅ | `crawler.go:1561` | `extractGraphQLOp()` from request body |
| API response callback | ✅ | `crawler.go:1548-1592` | Real-time callback saves to filesystem and WARC |

### Resource Fallback

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Missing resource tracking | ✅ | `interceptor.go:579-606` | `GetMissingResources()` returns URLs where body fetch failed |
| HTTP fallback download | ✅ | `interceptor.go:422-470` | `DownloadResourceViaHTTP()` — Go HTTP client fallback |
| Batch body fetch | ✅ | `interceptor.go:307-389` | `FetchBodies()` — process remaining pending resources |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **No network request blocking** | 🟡 Medium | Can intercept but not block/abort requests. Ads/trackers waste bandwidth. See [#21] |
| **No POST body for all methods** | 🟢 Low | Only non-GET POST body captured. GraphQL GET queries missed. |
| **No redirect chain handling** | 🟢 Low | `EventResponseReceived` fires before redirect response bodies available |
| **No loading failed handler** | 🟢 Low | `EventLoadingFailed` not handled — silent failures |
| **No request body for multipart** | 🟢 Low | `request.PostData` may miss `multipart/form-data` |

---

## 6. Page Capture Pipeline

Core pipeline in `crawler.go:1506-2289` (`doCrawl`) and `crawler.go:1989-2289` (`captureCurrentPage`).

### Full Pipeline (Order of Operations)

```
1. Create tab context (chromedp.NewContext)                          :1507
2. Inject stealth script (page.AddScriptToEvaluateOnNewDocument)     :1513-1519
3. Inject pushState capture script                                   :1522-1529
4. Setup console error listener                                      :1531
5. Setup WebSocket listener                                          :1532
6. Set config cookies                                                :1533
7. Inject persisted cookies from jar                                 :1534
8. Run form authentication (if enabled)                              :1536-1540
9. Start network interceptor                                         :1546-1593
10. Run JS-before-load scripts                                       :1595-1599
11. Navigate to URL via page.Navigate()                              :1601-1610
12. Wait for page ready (body + network idle)                        :1623
13. Get final URL after redirects                                    :1625-1634
14. Persist cookies from browser                                     :1635
15. Apply wait strategy (selector/networkidle/response/adaptive)     :1637
16. Dismiss overlays (cookie consents, modals)                       :1639-1641
17. Expand collapsible sections                                      :1642-1644
18. Click configured selectors                                       :1645-1656
19. Trigger lazy loading                                             :1657-1659
20. Run infinite scroll (if enabled)                                 :1661-1674
21. Run JS-after-load scripts                                        :1676-1680
22. Interactive prompt (if interactive mode)                          :1682-1684
23. Run interaction engine (if enabled)                              :1687-1689
24. Discover SPA routes + navigate to each                           :1691-1786
25. Extract shadow DOM                                               :1789-1799
26. Extract structured data (JSON-LD, OG, Twitter, meta)             :1801-1812
27. Detect and unregister service workers                            :1814-1818
28. Extract article content                                          :1820-1830
29. Generate SingleFile snapshot                                     :1832-1839
30. → captureCurrentPage()                                           :1841
31. Extract links from rewritten HTML → queue                        :1846-1882
32. Extract iframe sources → queue                                   :1885-1924
33. Extract media sources (video/audio/poster) → queue               :1927-1978
34. Fetch page metadata                                              :1980-1982
35. Close network interceptor                                        :1984
```

### captureCurrentPage Sub-Pipeline:1989-2289

```
1. Serialize shadow DOM into page HTML (JS evaluation)              :1991-2033
2. Memory budget reservation                                         :2035-2036
3. Change detection snapshot comparison                              :2038-2055
4. Solve CAPTCHAs                                                    :2058-2060
5. Detect JS framework                                               :2064-2070
6. Full-page screenshot (JPEG 80)                                    :2072-2077
7. PDF capture (printBackground=true)                                :2079-2092
8. Save HTML to filesystem                                           :2094-2097
9. Rewrite URL base                                                  :2099
10. Write HTML to WARC (if enabled)                                  :2101-2113
11. Save screenshot file                                             :2115-2116
12. Save PDF file                                                    :2118-2119
13. Fetch remaining response bodies                                  :2122
14. Process intercepted resources (dedup, save, map)                 :2124-2175
15. HTTP-fallback for missing resources                              :2177-2201
16. Download HTML-discovered assets (HTML tokenizer)                  :2203-2205
17. Extract font @font-face from CSS, download                       :2210-2244
18. Extract CSS @import URLs, download (recursive)                   :2246-2278
19. Process CSS files (rewrite URLs)                                 :2281-2283
20. Resolve JS dependencies (import(), require, webpack, etc.)       :2285
21. Final HTML rewrite with relative paths                           :2286
```

### Output Formats Captured

| Format | Status | File:Line | Implementation |
|---|---|---|---|
| Static HTML | ✅ | `filesystem.go:166-193` | `.html` with relative paths |
| Full-page screenshot (PNG) | ✅ | `crawler.go:2074-2077` | `chromedp.FullScreenshot` JPEG quality 80 |
| PDF | ✅ | `crawler.go:2080-2092` | `page.PrintToPDF` with printBackground |
| SingleFile HTML | ✅ | `jsengine/scripts.go:966-970` | Inline all CSS/images/scripts as data URLs |
| WARC | 🟡 | `storage/warc.go:45-71` | WARC 1.0, gzip, **buggy** curSize tracking |
| Shadow DOM JSON | ✅ | `filesystem.go:217-226` | tag + innerHTML per shadow root |
| API response JSON | ✅ | `crawler.go:1587-1591` | Path-based JSON files |
| Article JSON | ✅ | `crawler.go:1820-1830` | Readability extraction |
| Structured data JSON | ✅ | `crawler.go:1801-1812` | JSON-LD + OG + Twitter + meta |
| JS errors JSON | ✅ | `crawler.go:595` | `js-errors.json` |
| WebSocket messages JSON | ✅ | `crawler.go:596` | `websocket.json` |
| File index JSON | ✅ | `filesystem.go:238-253` | `index.json` URL→path mapping |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **WARC writer size tracking broken** | 🔴 Critical | `curSize` tracks gzip-compressed bytes, not content bytes. Rotation never triggers correctly. See [Bug #1] |
| **No WACZ output** | 🟡 Medium | Industry standard format with dedup + CDX index. See [#15] |
| **No HAR output** | 🟢 Low | HTTP Archive format not generated |
| **No content-addressed storage** | 🟢 Low | Cross-crawl dedup not possible |

---

## 7. Navigation & Waiting

All in `crawler.go` and `jsengine/wait.go`.

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Page navigation | ✅ | `crawler.go:1601-1610` | `page.Navigate()` via CDP |
| Redirect detection | ✅ | `crawler.go:1625-1634` | `window.location.href` comparison |
| Wait for body ready | ✅ | `crawler.go:2692-2703` | `chromedp.WaitReady("body")` |
| Wait for network idle | ✅ | `jsengine/scripts.go:421-451` | PerformanceObserver-based JS |
| Wait for selector | ✅ | `jsengine/scripts.go:402-419` | Polling with timeout |
| Wait for response URL | ✅ | `jsengine/wait.go:22-50` | PerformanceObserver URL pattern matching |
| Adaptive wait (framework-aware) | ✅ | `crawler.go:3456-3468` | Detect framework → framework-specific wait |

### Wait Strategies:3457-3479

| Strategy | Status | Implementation |
|---|---|---|
| `selector` | ✅ | Wait for CSS selector to appear in DOM |
| `networkidle` | ✅ | Wait for no network activity for quiet period |
| `response` | ✅ | Wait for matching response URL pattern |
| `adaptive` | ✅ (default) | Detect framework → use framework-specific wait. Falls back to networkidle |
| (default/timer) | ✅ | Simple timer wait |

### Overlay & Interaction Pre-Capture:1639-1659

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Dismiss cookie consent | ✅ | `jsengine/scripts.go:908-951` | 9 cookie selectors (onetrust, cookie-first, etc.) |
| Dismiss modals | ✅ | `jsengine/scripts.go:908-951` | `.modal-close`, `[data-dismiss="modal"]`, `[aria-label="Close"]` |
| Remove fixed overlays | ✅ | `jsengine/scripts.go:942-948` | Elements with z-index > 1000 or modal/overlay class |
| Expand sections | ✅ | `jsengine/scripts.go:890-906` | `[data-toggle]`, `[aria-expanded=false]`, `details`, `.collapsible` |
| Click configured selectors | ✅ | `crawler.go:1645-1656` | Configurable `ClickSelectors` with 500ms delay |
| Lazy loading trigger | ✅ | `jsengine/scripts.go:174-191` | img[data-src], [data-bg], iframe[data-src] |
| Custom JS before/after load | ✅ | `crawler.go:1595-1599,1676-1680` | Configurable `JSBeforeLoad`/`JSAfterLoad` |

---

## 8. Interaction Engine

All in `crawler.go:2728-3040`.

### Element Discovery:2749-2856

The engine discovers interactive elements via 22+ CSS selectors executed in the page:

```javascript
'button:not([disabled])',
'a[href]:not([href^="mailto:"]):not([href^="tel:"]):not([href^="javascript:"]):not([target="_blank"])',
'input[type="button"]:not([disabled])',
'input[type="submit"]:not([disabled])',
'[role="button"]:not([aria-disabled="true"])',
'[onclick]',
'.btn:not([disabled])',
'button.btn:not([disabled])',
'[data-action]', '[data-click]', '[data-toggle]',
'[data-dismiss]', 'summary',
'details:not([open]) > summary',
'.accordion-header', '.accordion-trigger',
'[aria-expanded="false"]',
'.collapsible:not(.active)',
'[data-bs-toggle="collapse"]',
'[data-bs-toggle="modal"]',
'[data-toggle="tab"]', '[data-toggle="pill"]'
```

Plus form and input discovery.

### Interaction Types

| Type | Status | Implementation |
|---|---|---|
| Click element | ✅ | `el.click()` via JS Evaluate |
| Fill input | ✅ | Static values: `"test"`, `"test@example.com"`, `"testpassword123"`, `"test query"` |
| Form submission | ❌ | `interactWithForm()` returns `false` — **no-op**. See [Bug #6] |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **interactWithForm returns false** | 🔴 Critical | Forms detected and logged but never submitted. Core feature is broken. See [Bug #6] |
| **No scroll-into-view before click** | 🟡 Medium | Element may be off-screen, `el.click()` may fail |
| **No context-aware form values** | 🟢 Low | Static "test" values are detectable and insufficient |
| **No aria-expanded verification** | 🟢 Low | No check that click actually changed state |
| **No mobile gestures** | 🟢 Low | No swipe/long-press/touch patterns |
| **target=_blank links excluded** | 🟢 Low | Links that open in new tab are skipped |

---

## 9. Infinite Scroll

All in `jsengine/scroll.go:36-189`.

| Feature | Status | Default | Implementation |
|---|---|---|---|
| Scroll to bottom | ✅ | — | `window.scrollTo(0, document.body.scrollHeight)` |
| Scroll by distance | ✅ | 500px | Configurable step scroll |
| Item counting | ✅ | `article,.card,.list-item,[data-infinite-scroll-item],.feed-item` | Configurable selector |
| Stability detection | ✅ | 3 passes | Configurable stable passes |
| Load More button clicking | ✅ | — | Text matching: "Load More", "Show More", etc. |
| Max scrolls | ✅ | 20 | Configurable |
| Max duration | ✅ | 10s | Configurable |
| Scroll delay | ✅ | 2s | Configurable |
| Result tracking | ✅ | — | TotalItems, TotalScrolls, NewItemsFound, ItemsPerScroll |
| Scroll container | ✅ | document.body | Configurable alternative container |

---

## 10. SPA Route Discovery

All in `crawler.go:1691-1786` and `jsengine/scripts.go:248-295,453-511`.

### Framework Detection

| Framework | Detection Method |
|---|---|
| Next.js | `window.__NEXT_DATA__` |
| Nuxt | `window.__NUXT__` |
| Angular | `[ng-version]` attribute |
| React | `[data-reactroot]` or `#__next` |
| Vue | `[data-v-]` or `window.__VUE__` |
| Svelte | `[class*="svelte-"]` |

### Route Interception

| Method | Status | Implementation |
|---|---|---|
| pushState | ✅ | `history.pushState` override, stores in `__pushStateRoutes` |
| replaceState | ✅ | `history.replaceState` override |
| hashchange | ✅ | `window.addEventListener('hashchange')` |
| popstate | ✅ | `window.addEventListener('popstate')` |
| Link discovery | ✅ | `document.querySelectorAll('a[href]')` |

### Route Processing:1691-1786

- Routes discovered from pushState events and `<a href>` links
- Resolved to absolute URLs via `rewrite.ResolveURL()`
- New routes queued for crawling (up to `MaxSPARoutes`, default: 50)
- The crawler navigates to each SPA route in the **SAME** tab
- Waits for hydration: network idle + 2s
- Captures HTML and saves

### Issues

| Issue | Severity | Details |
|---|---|---|
| **Single tab for all SPA routes** | 🟡 Medium | Navigations sequential in same tab — very slow |
| **No route parameterization** | 🟢 Low | Dynamic routes like `/posts/:id` not discovered |
| **No data store extraction** | 🟢 Low | Redux/Vuex state, React context, route data not captured |

---

## 11. Authentication

All in `internal/auth/auth.go` (313 lines).

| Method | Status | File:Line | Implementation |
|---|---|---|---|
| Form login | ✅ | `auth.go:61-147` | Navigate to login URL, fill fields from config, click submit |
| HTTP Basic auth | ✅ | `auth.go:149-163` | `network.SetExtraHTTPHeaders` with Authorization: Basic |
| Custom header auth | ✅ | `auth.go:165-182` | `network.SetExtraHTTPHeaders` with custom headers |
| OAuth 2.0 client credentials | ✅ | `auth.go:184-266` | Token exchange via HTTP POST, inject as Authorization: Bearer |
| Interactive pre-crawl login | ✅ | `crawler.go:412-442` | Show browser, user logs in, press Enter |
| Cookie persistence | ✅ | `crawler.go:3044-3088` | Cookies saved to `cookies.json`, loaded on restart |
| Cookie injection via CDP | ✅ | `crawler.go:3132-3188` | `network.SetCookie()` per cookie |
| Session validity check | ✅ | `auth.go:280-292` | `HasValidSession()` checks cookie expiry |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **OAuth refresh token unused** | 🟢 Low | `refresh_token` stored but never used for token refresh |
| **Cookie domain matching** | 🟡 Medium | Domain split by "." produces false negatives. See [Bug #5] |

---

## 12. CAPTCHA Handling

All in `internal/captcha/solver.go` (347 lines) and `crawler.go:3549-3697`.

### Supported Providers

| Provider | Status | URL |
|---|---|---|
| 2Captcha | ✅ | `https://2captcha.com` |
| AntiCaptcha | ✅ | `https://api.anti-captcha.com` |
| CapMonster | ✅ | `https://api.capmonster.cloud` |

### Supported Types

| Type | Status |
|---|---|
| reCAPTCHA v2 | ✅ |
| reCAPTCHA v3 | ✅ |
| hCaptcha | ✅ |
| Cloudflare Turnstile | ✅ |
| Image CAPTCHA | ✅ |
| Generic (data-sitekey) | ✅ |
| Script-based detection | ✅ |

### Detection & Solving Pipeline

1. **Detection** (`crawler.go:3610-3675`):
   - Check for `.g-recaptcha`, `.h-captcha`, `.cf-turnstile` elements
   - Check for `[data-sitekey]` on any element
   - Check for loaded CAPTCHA scripts `<script src="...recaptcha...">`
2. **Solving** (`crawler.go:3549-3608`):
   - Create `SolveRequest` with URL, sitekey, page HTML
   - Call external API (poll for result)
   - Retry up to `RetryCount` with 2s backoff
3. **Injection** (`crawler.go:3677-3697`):
   - reCAPTCHA: `g-recaptcha-response` innerHTML
   - hCaptcha: `data-hcaptcha-response` innerHTML
   - Turnstile: `data-turnstile-response` innerHTML
4. **Post-solve**: 2s wait for CAPTCHA re-evaluation

---

## 13. Content Extraction

### Structured Data

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| JSON-LD | ✅ | `jsengine/scripts.go:574-598` | `script[type="application/ld+json"]` extraction |
| Open Graph | ✅ | `jsengine/scripts.go:582-585` | `meta[property^="og:"]` |
| Twitter Cards | ✅ | `jsengine/scripts.go:586-589` | `meta[name^="twitter:"]` |
| Generic Meta Tags | ✅ | `jsengine/scripts.go:590-596` | `meta[name]` and `meta[property]` (non-OG/Twitter) |
| JSON data extraction | ✅ | `jsengine/intercept.go:11-28` | `ExtractJSONFromPage()` by CSS selector |

### Content

| Feature | Status | Implementation |
|---|---|---|
| Article extraction | ✅ | Readability-style: article→[role=main]→.content→#content→largest text container |
| Shadow DOM extraction | ✅ | `querySelectorAll('*')` → `el.shadowRoot.innerHTML` |
| Iframe source extraction | ✅ | `querySelectorAll('iframe, frame')` src + data-src fallback |
| Media source extraction | ✅ | video/audio src + source children + poster |

### JS Dependency Resolution

All in `internal/jsanalyzer/analyzer.go` (229 lines).

| Pattern | Status | Implementation |
|---|---|---|
| dynamic import() | ✅ | `import("...")` regex |
| require() | ✅ | `require("...")` regex |
| fetch() | ✅ | `fetch("...")` regex |
| XHR .open() | ✅ | `.open("METHOD", "...")` regex |
| importScripts() | ✅ | Worker importScripts |
| System.import() | ✅ | SystemJS |
| axios.get/post/... | ✅ | Axios HTTP calls |
| $.ajax() | ✅ | jQuery AJAX |
| defineAsyncComponent | ✅ | Vue async components |
| React.lazy() | ✅ | React lazy imports |
| Vue.component() lazy | ✅ | Vue lazy components |
| webpack chunk names | ✅ | webpackChunkName comments |
| import maps | ✅ | `<script type="importmap">` |
| module scripts | ✅ | `<script type="module" src="...">` |
| **Recursion** | ✅ | Up to 3 levels deep |

---

## 14. Storage & Output

All in `internal/storage/filesystem.go` (253 lines), `storage/warc.go` (231 lines).

### Filesystem Storage:filesystem.go

| Feature | Status | Implementation |
|---|---|---|
| Per-host directory | ✅ | `outputDir/hostname/path.html` |
| URL path → file path | ✅ | Clean and decode URL path |
| Query string handling | ✅ | Query params appended (sanitized) to filename |
| Directory index (paths ending in /) | ✅ | `index.html` for directory-style paths |
| Path extension detection | ✅ | Paths without extensions get `/index.html` appended |
| Path traversal prevention | ✅ | Double-checked output directory containment |
| API response paths | ✅ | `PathForAPI()` with .json extension |
| File index | ✅ | `index.json` with URL→path→sha256→mime mapping |

### WARC Writer:warc.go

| Feature | Status | Implementation |
|---|---|---|
| WARC 1.0 format | ✅ | Standard headers, block digest, content-length |
| gzip compression | ✅ | `compress/gzip` writer |
| File rotation | 🟡 | **Buggy** — See [Bug #1] |
| warcinfo record | ✅ | Software identity and format metadata |
| UUID generation | ✅ | crypto/rand based |
| Concurrent writes | ✅ | sync.Mutex guard |
| Max file size | ✅ | 1GB (but tracking is wrong) |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **WARC curSize tracks compressed size** | 🔴 Critical | `n` from `gzip.Write()` is compressed bytes, not payload size. Rotation check `curSize >= maxSize` uses wrong metric. 1GB file rotation never triggers correctly. See [Bug #1] |

---

## 15. Crawling Infrastructure

### Rate Limiting & Resilience

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Per-host token bucket | ✅ | `ratelimit/` | Dynamic capacity from robots.txt crawl-delay |
| Latency observation | ✅ | `crawler.go:550-552` | `ObserveLatency()` for adaptive rate limiting |
| Circuit breaker (3-state) | ✅ | `resilience/` | Closed→half-open→open per host |
| Exponential backoff with jitter | ✅ | `crawler.go:1447` | Retry with backoff |
| Retry classification | ✅ | `crawlerrors/` | 14 error kinds, retryable flags |

### Deduplication

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Bloom filter (probabilistic) | ✅ | `crawler.go:192-202` | Expected items: MaxTotalURLs * 10, false positive rate: 1% |
| Bloom filter persistence | ✅ | `crawler.go:198-202` | Load from file on startup |
| LRU exact dedup | ✅ | `crawler.go:255` | Size: max(MaxTotalURLs, 100k) |
| Content hash dedup | ✅ | `crawler.go:2130` | XXHash, 200k LRU |
| Bloom file save | ✅ | `BloomFilterPath` | Optional file persist |

### Queue & Scheduling

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| BFS priority queue (heap) | ✅ | `queue/` | Depth-based ordering |
| Memory queue | ✅ | `queue/local.go` | Default: in-memory |
| Persistent (disk) queue | ✅ | `queue/persistent.go` | File-backed |
| Redis queue | ✅ | `queue/redis.go` | Redis list |
| PostgreSQL queue | ✅ | `queue/postgres.go` | PG table |
| Kafka queue | ✅ | `queue/kafka.go` | Kafka topic |
| Queue config swap | ✅ | `crawler.go:215-219` | `queue.NewQueueFromConfig()` |
| Max queue size | ✅ | `crawler.go:84` | `maxQueueSize = 100,000` |

### Checkpoint & Resume

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Periodic checkpoint | ✅ | `crawler.go:3516-3531` | Configurable interval (default: 5min) |
| Gob encoding | ✅ | `checkpoint.go` | Binary serialization |
| Atomic rename | ✅ | `checkpoint.go` | Temp file + rename pattern |
| Resume from checkpoint | ✅ | `crawler.go:330-343` | Load queue + visited on start |
| **Race condition** | 🔴 | `crawler.go:1367` | See [Bug #2] |

### Incremental Crawling

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| ETag caching | ✅ | `inc_cache.go` | Store ETags from responses |
| Last-Modified caching | ✅ | `inc_cache.go` | Store Last-Modified headers |
| Conditional requests | ✅ | `crawler.go:2126-2127` | 304 handling |
| Cache file persistence | ✅ | `IncCacheFile` | File-based cache |

### Robots.txt

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| robots.txt parser | ✅ | `robots/robots.go:1-376` | Wildcard patterns, user-agent matching |
| Crawl-delay extraction | ✅ | `robots.go` | Parse Crawl-Delay directive |
| Sitemap discovery | ✅ | `robots.go` | Parse Sitemap directive |
| 24h cache | ✅ | `robots.go:43` | `cacheTTL = 24 * time.Hour` |
| Singleflight dedup | ✅ | `robots.go:36` | `singleflight.Group` for concurrent fetches |

### Error Handling

| Feature | Status | File:Line | Implementation |
|---|---|---|---|
| Error classification | ✅ | `errors/` | 14 error kinds (timeout, network, DNS, TLS, HTTP, rate-limit, blocked, auth, parse, resource, browser, OOM, cancelled) |
| Retryable vs non-retryable | ✅ | `errors/` | Per-kind retryability classification |
| Panic recovery | ✅ | `crawler.go:554-566` | Per-goroutine recover with stack trace |
| **Error inconsistency** | 🟡 | `crawler.go:1614` vs `retry.go` | 5xx classified differently. See [Issue #8] |

### Periodic Tasks

| Task | Status | Interval | File:Line |
|---|---|---|---|
| Checkpoint save | ✅ | 5min (default) | `crawler.go:3516-3531` |
| Progress report | ✅ | 30s | `crawler.go:406` |
| Cookie jar save | ✅ | 5min | `crawler.go:3059-3071` |
| Stale map cleanup | ✅ | 5min | `crawler.go:3481-3492` |
| Memory GC pressure | ✅ | >80% budget | `crawler.go:493-497` |

### Issues

| Issue | Severity | Details |
|---|---|---|
| **Checkpoint race condition** | 🔴 Critical | `saveCheckpoint()` reads queue without lock while goroutines push. See [Bug #2] |
| **Queue pointer aliasing** | 🟡 Medium | `Items()` returns values but heap stores pointers. See [Bug #4] |
| **Retry classification inconsistency** | 🟡 Medium | 5xx errors classified as retryable in `retry.go` but mixed in `doCrawl` navigation handler. See [Issue #8] |
| **Dedup logic duplicated 4x** | 🟢 Low | Same bloom/LRU/allowed/exclude/max check repeated for links, iframes, media, poster. See [Issue #13] |
| **No distributed coordination** | 🟡 Medium | Queue backends exist but no worker pool coordination, leader election |

---

## 16. CLI & Configuration

All in `cmd/clone/main.go` (242 lines) and `internal/config/config.go` (367 lines).

### CLI Flags

| Flag | Short | Default | Config Field |
|---|---|---|---|
| `--config` | `-c` | `""` | Config file path |
| `--depth` | `-d` | `10` | `MaxDepth` |
| `--concurrency` | `-n` | `5` | `MaxConcurrentPages` |
| `--output` | `-o` | `"output"` | `OutputDir` |
| `--screenshot` | `-s` | `false` | `EnableScreenshot` |
| `--pdf` | `-p` | `false` | `EnablePDF` |
| `--proxy` | | `""` | `Proxy` |
| `--timeout` | | `120s` | `PageTimeout` |
| `--stealth` | | `true` | `EnableStealth` |
| `--no-robots` | | `false` | `RespectRobots` (inverted) |
| `--delay` | | `1s` | `CrawlDelay` |
| `--max-urls` | | `10000` | `MaxURLsPerHost` |
| `--scroll` | | `true` | `InfiniteScroll.Enabled` |
| `--interact` | | `false` | `EnableInteractionEngine` |
| `--interactive` | | `false` | `Interactive` |
| `--manual-capture` | | `false` | `ManualCapture` |
| `--help` | `-h` | | Help text |
| `--version` | | `1.0.0` | Version string |

### Serve Command

| Flag | Default | Description |
|---|---|---|
| `--port` | `8080` | HTTP server port |

Serve features:
- CORS headers (Access-Control-Allow-Origin: *)
- Fallback routing (missing file → try `index.html`)
- JSON fallback (missing file → try `.json` extension)
- Host directory auto-detection

### Config Validation

25+ validation rules across all subsystems:
- Seeds required (unless manual-capture mode)
- MaxDepth > 0
- MaxConcurrentPages > 0
- PageTimeout > 0
- CrawlDelay >= 0
- MaxURLsPerHost > 0
- MaxTotalURLs > 0
- CheckpointInterval >= 0
- WaitStrategyTimeout >= WaitTimeout
- Auth: form requires login_url + username + password
- Auth: basic requires username + password
- Auth: header requires at least one header
- Auth: oauth requires client_id + client_secret + token_url
- CAPTCHA requires api_key + provider + timeout > 0
- Queue: redis requires redis_url
- Queue: postgres requires pg_dsn
- Queue: kafka requires kafka_url
- Change detection: max_snapshots > 0

---

## 17. Backend Queue Integrations

### Redis Queue

| File | `internal/queue/redis.go` |
|---|---|
| Data structure | Redis List (LPUSH/BRPOP) |
| Persistence | Redis persistence |
| Distributed | Yes (shared Redis) |

### PostgreSQL Queue

| File | `internal/queue/postgres.go` |
|---|---|
| Data structure | PostgreSQL table with priority |
| Persistence | PG durability |
| Distributed | Yes (shared PG) |

### Kafka Queue

| File | `internal/queue/kafka.go` |
|---|---|
| Data structure | Kafka topic |
| Persistence | Kafka log |
| Distributed | Yes (shared Kafka) |

---

## 18. Third-Party Integrations

| Integration | Status | Type |
|---|---|---|
| Chrome/Chromium (chromedp) | ✅ | Browser engine |
| 2Captcha | ✅ | CAPTCHA solving |
| AntiCaptcha | ✅ | CAPTCHA solving |
| CapMonster | ✅ | CAPTCHA solving |
| SingleFile (inlined) | ✅ | Page serialization |
| Redis | ✅ | Queue backend |
| PostgreSQL | ✅ | Queue backend |
| Kafka | ✅ | Queue backend |
| Service Worker (generated) | ✅ | Offline replay |
| WebSocket Replay (generated) | ✅ | Offline replay |
| Docker | ❌ | Not available |
| Kubernetes/Helm | ❌ | Not available |
| Prometheus | ❌ | Not available |
| OpenTelemetry | ❌ | Not available |
| Browserless.io | ❌ | Not available |
| Playwright | ❌ | Not available |
| Puppeteer | ❌ | Not available |

---

## 19. Bugs & Issues

### Bug #1: WARC Writer Size Tracking (🔴 Critical)

**File:** `internal/storage/warc.go:56-61`

**Problem:**
```go
n, err := w.gzipW.Write(record)  // n = compressed bytes
w.curSize += int64(n)             // tracks compressed size
if w.curSize >= w.maxSize {       // compares compressed vs 1GB
    // rotate — never triggers correctly
}
```

`gzip.Writer.Write()` returns the number of compressed bytes written, not the uncompressed payload length. The `formatWARCRecord()` function builds records with `Content-Length: <uncompressed payload>`, but `curSize` tracks compressed bytes. Since compression reduces size, `curSize` grows slower than actual content, so the 1GB file rotation limit may be reached far later than intended (or never, depending on compression ratio).

**Fix:** Track uncompressed payload length separately. Either return it from `formatWARCRecord()` or compute it before gzip.

---

### Bug #2: Checkpoint Race Condition (🔴 Critical)

**File:** `internal/crawler/crawler.go:1356-1368`

**Problem:**
```go
func (c *Crawler) saveCheckpoint() {
    c.hostMu.RLock()
    // snapshot hostLastCrawl and hostURLCount under lock
    c.hostMu.RUnlock()
    c.checkpoint.Save(c.urlQueue, hlc, huc)  // urlQueue read WITHOUT lock
}
```

The queue is concurrently pushed to by crawling goroutines (line 1881: `c.urlQueue.PushURL(...)`). There is no mutex protecting the queue read during checkpoint. This is a data race: the queue's internal state (heap, slice) can be mutated during iteration.

**Fix:** Add a `sync.RWMutex` to the Queue interface or snapshot queue items atomically.

---

### Bug #3: Browser Restart Deadlock (🔴 Critical)

**File:** `internal/crawler/crawler.go:1406-1436`

**Problem:**
```go
func (c *Crawler) restartBrowser() context.Context {
    c.browserMu.Lock()                              // LOCK HELD
    // ...
    allocCtx, allocCancel := chromedp.NewExecAllocator(c.ctx, c.allocOpts...)  // MAY BLOCK
    browserCtx, browserCancel := chromedp.NewContext(allocCtx)
    if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
        // MORE BLOCKING OPS
    }
    c.browserMu.Unlock()
}
```

`chromedp.NewExecAllocator()` spawns Chrome and connects via CDP — may block if Chrome doesn't start, port is busy, or Chrome is unresponsive. All goroutines calling `getBrowserCtx()` (which acquires the same mutex) block during this time. If all goroutines are blocked and the crawl loop can't proceed, the entire crawl hangs.

**Fix:** Add timeout context for the allocator, or move blocking operations outside the lock using channels.

---

### Bug #4: Queue Interface Leak / Pointer Aliasing (🟡 Medium)

**File:** `internal/queue/interface.go`

**Problem:** The queue heap stores `*URLItem` pointers. The `Items()` method returns slice copies of pointer values. While the returned slice is a copy, the internal heap still holds the same pointers. This means if an external caller modifies the items they got (unlikely since they're value copies), the internal state is unaffected. However, the heap operations (Pop) return `*URLItem` pointers that alias the same memory. This is more a design smell than active bug.

**Fix:** Use Go generics (`[]URLItem` instead of `[]*URLItem`) to eliminate pointer sharing entirely.

---

### Bug #5: Cookie Domain Matching (🟡 Medium)

**File:** `internal/crawler/crawler.go:3145-3147`

**Problem:**
```go
parts := strings.Split(domain, ".")
for i := 0; i < len(parts); i++ {
    d := strings.Join(parts[i:], ".")
    for _, ck := range c.cookieJar[d] { ... }
}
```

For domain `sub.example.com`, parts = `["sub", "example", "com"]`, tries: `"sub.example.com"`, `"example.com"`, `"com"`. This works. But it doesn't handle the case where a cookie domain like `"x.com"` is a suffix of `"yxx.com"` incorrectly — this is actually fine because the split produces different starting positions. The **real bug** is that the split treats TLDs like `"com"` as a valid domain to check, which is always a wasted lookup. Not a security issue but wastes lookups and could cause false positives if someone stores cookies under `"com"` or `"co.uk"` keys.

**Fix:** Use proper public suffix matching (golang.org/x/net/publicsuffix) or at minimum skip iteration when remaining parts have no dot.

---

### Bug #6: interactWithForm is No-Op (🔴 Critical)

**File:** `internal/crawler/crawler.go:2975-2987`

**Problem:**
```go
func (c *Crawler) interactWithForm(ctx context.Context, xpath string, item map[string]interface{}) bool {
    action := ""   // read from item
    method := ""   // read from item
    util.LogDebug("interaction: found form", ...)
    return false   // ALWAYS returns false — form never submitted
}
```

Forms are discovered, logged, but never filled or submitted. The interaction engine skips them because `handled` stays false. This means the interaction engine cannot handle any form on any page.

**Fix:** Implement form field filling (use existing values from the page or apply filler heuristics), then submit via `form.submit()` or click the submit button.

---

### Issue #7: No Graceful Chrome Shutdown (🟡 Medium)

**File:** `internal/crawler/crawler.go:1393-1396`

**Problem:** `browserCancel` cancels the allocator context which kills Chrome, but the Go process for Chrome is never `Wait()`ed on. This leaves zombie processes that consume PID slots.

**Fix:** Store Chrome's `*os.Process` and call `process.Wait()` during `Stop()`.

---

### Issue #8: Retry Classification Inconsistency (🟡 Medium)

**Files:** `internal/crawler/crawler.go:1612-1620`, `internal/errors/retry.go`

**Problem:** `doCrawl` has ad-hoc string matching for HTTP status codes in navigation errors, while `retry.go` has a proper retry classification system. They disagree on what's retryable. The string-based check at line 1614 treats 4xx as non-retryable but doesn't handle 5xx distinctly. The error classification system may classify the same error differently.

**Fix:** Replace the string matching with a call to `crawlerrors.Classify()`.

---

### Issue #9: No Context Deadlines on Chromedp.Run (🟡 Medium)

**Locations:** `crawler.go:1514,1596,1626,1677,1773,1991,2009,2025,2032,2040,2074,2081,2959,2960` and more

**Problem:** Multiple `chromedp.Run(tabCtx, ...)` calls use `tabCtx` which has the page timeout, but some calls like `chromedp.Title`, `chromedp.OuterHTML`, `chromedp.Evaluate` may use contexts that are near expiry or already expired. These can hang indefinitely if the CDP connection is broken.

**Fix:** Create a helper `c.shortContext(duration)` that returns a fresh context with timeout, and use it for all `chromedp.Run` calls.

---

### Issue #10: Empty Error Handling (🟢 Low)

**Locations:** Multiple across codebase

**Problem:** Pattern `io.Copy(io.Discard, resp.Body)` silently swallows errors at many resource-download sites. `os.WriteFile(path, data, 0644)` errors ignored at lines 1589, 1808, 1827, 1837, 3056. These make debugging resource download failures impossible.

**Fix:** Log errors at minimum. Better: aggregate and report at crawl end.

---

### Issue #11: No WebSocket Frame Size Limit (🟢 Low)

**File:** `internal/crawler/crawler.go:3231-3235`

**Problem:** Binary WebSocket frames are stored with no size cap. A single large binary WS message (e.g., video stream) could OOM the process.

**Fix:** Add configurable `MaxWSFrameSize` (default: 10MB), truncate or skip frames exceeding it.

---

### Issue #12: No CSS @import Cycle Detection (🟢 Low)

**File:** `internal/crawler/crawler.go:2246-2278`

**Problem:** CSS @import resolution follows URLs and marks files as processed, but circular @import chains (A→B→C→A) could cause infinite loops in the recursive processing (`processCSSImports` in rewrite module).

**Fix:** Add visited URL set before processing CSS imports, skip already-visited URLs.

---

### Issue #13: Duplicate Dedup Logic (🟢 Low)

**File:** `internal/crawler/crawler.go:1855-1978`

**Problem:** The same 12-line URL dedup / queue push pattern is repeated verbatim 4+ times:
- Links (line 1855-1882)
- Iframe sources (line 1885-1924)
- Media sources (line 1927-1963)
- Poster URLs (line 1965-1975)

Small variations make maintenance error-prone. Adding a new URL source type requires copying the same block again.

**Fix:** Extract `shouldQueue(rawURL string) bool` method.

---

## 20. Missing Features

### Critical Missing Features (Blocking Production Use)

| # | Feature | Need | Approach | Status |
|---|---|---|---|---|
| 15 | **WACZ output** | WARC is buggy; WACZ is industry standard | Add WACZ writer: CDX index + compressed WARC records in ZIP | 🔴 Pending |
| 16 | **Browser profiles** | Every restart = fresh profile; lost sessions | Add `--user-data-dir` config, pass as Chrome flag, support named profiles | 🔴 Pending |
| 19 | **Docker support** | Essential for production deployment | Multi-stage Dockerfile + docker-compose + Helm chart | 🔴 Pending |
| 21 | **Network request blocking** | Ads/waste bandwidth | `network.SetBlockedURLs` or `Fetch.enable` with abort | 🔴 Pending |

### Medium Priority Missing Features

| # | Feature | Why |
|---|---|---|
| 22 | Minimal Web UI dashboard | No crawl monitoring without CLI logs |
| 23 | REST API | No programmatic control |
| 24 | Crawl scheduling (cron) | No recurring crawl capability |
| 25 | Notifications (webhook/email/slack) | No alerts on completion/errors/change detection |
| 26 | Enhanced stealth (canvas/font/WebRTC) | Current stealth detectable by sophisticated bots |
| 27 | Context-aware form filling (AI) | Static "test" values are insufficient |
| 28 | SPA data store capture (Redux/Vuex) | Critical app state not in DOM |
| 29 | OAuth token refresh | Token expires but refresh_token stored but unused |
| 30 | Tab pool | New CDP context per page is wasteful |

### Niche / Future Missing Features

| # | Feature |
|---|---|
| 31 | Plugin/behavior system (extensible JS behaviors) |
| 32 | Screencasting (live browser view via WebSocket) |
| 33 | LLM-optimized (markdown) output |
| 34 | Social media behaviors (Twitter scroll, Instagram load-more, TikTok) |
| 35 | Performance budgets (Lighthouse integration) |
| 36 | Git-native storage (version-controlled archiving) |
| 37 | WebRTC stream capture |
| 38 | HTTP/3 + QUIC support for non-browser downloads |
| 39 | Mobile emulation (device metrics, touch events) |
| 40 | CAPTCHA solving via ML (bypass external API) |
| 41 | Distributed worker coordination (leader election, work stealing) |
| 42 | Prometheus / OpenTelemetry metrics |
| 43 | Playwright integration as alternative browser backend |
| 44 | Accessibility capture (ARIA tree, focus order) |
| 45 | Multi-language extraction (OCR, subtitle extraction) |

---

## 21. Phase Plan

### Phase 0 — Fix Critical Bugs (P0) — ✅ COMPLETED

| # | Item | Effort | Status |
|---|---|---|---|
| 1 | Fix WARC `curSize` tracking | ~1h | ✅ Done |
| 2 | Fix checkpoint race condition | ~1h | ✅ Done |
| 3 | Fix browser restart deadlock | ~1h | ✅ Done |
| 6 | Fix `interactWithForm` no-op | ~2h | ✅ Done |
| 5 | Fix cookie domain matching | ~30m | ✅ Done |
| 7 | Add graceful Chrome shutdown | ~1h | ✅ Done |

### Phase 1 — Core Reliability (P1) — ✅ COMPLETED

| # | Item | Effort | Status |
|---|---|---|---|
| 14 | Multi-browser pool | ~5h | ✅ Done |
| 13 | Dedup DRY refactor | ~30m | ✅ Done |
| 8 | Fix retry classification inconsistency | ~30m | ✅ Done |
| 9 | Add context deadlines to CDP calls | ~1h | ✅ Done |
| 12 | Add CSS @import cycle detection | ~15m | ✅ Done |
| 10 | Error handling cleanup | ~1h | ✅ Done |
| 11 | WS frame size limit | ~30m | ✅ Done |

### Phase 2 — Feature Completeness (P2) — 🟡 PARTIALLY COMPLETED

| # | Item | Effort | Status |
|---|---|---|---|
| 15 | WACZ output | ~5h | 🔴 Pending |
| 17 | Configurable Chrome flags | ~30m | ✅ Done |
| 18 | Remote Chrome connection | ~1h | ✅ Done |
| 21 | Network request blocking | ~2h | 🔴 Pending |

**Remaining:** WACZ output, Network request blocking

### Phase 3 — Operational Maturity (P3)

| # | Item | Effort | Dependencies |
|---|---|---|---|
| 19 | Docker support | ~3h | None |
| 16 | Browser profiles | ~2h | Phase 1 (multi-browser pool) |
| 22 | Minimal Web UI | ~4h | Phase 1 |
| 20 | Enhanced stealth | ~3h | None |
| 27 | Context-aware form filling | ~2h | Phase 0 form fix |
| 28 | Tab pool | ~2h | Phase 1 |

**Total:** ~16 hours

### Phase 4 — Premium / Niche (P4+)

Items 23-45 as time/resources allow.

---

## Appendix: File Reference

```
cmd/clone/main.go         — CLI entry, cobra commands, serve subcommand
internal/
  auth/auth.go            — Authentication manager (form, basic, header, oauth)
  captcha/solver.go       — CAPTCHA solving (2captcha, anticaptcha, capmonster)
  config/config.go        — Configuration struct, validation, defaults
  crawler/
    crawler.go            — Core crawler (~3700 lines), all browser interactions
    redirect.go           — HTTP redirect handling
    retry.go              — Retry configuration
  errors/
    errors.go             — Error classification (14 kinds)
    retry.go              — Retry classification
  httpclient/
    client.go             — HTTP client pool
  jsanalyzer/
    analyzer.go           — JS dependency URL extraction (import, require, webpack, etc.)
  jsengine/
    intercept.go          — JSON extraction from page
    scripts.go            — All JS injection scripts (~1071 lines)
    scroll.go             — Infinite scroll implementation
    scripts_test.go       — JS engine tests
    serviceworker.go      — Service worker manager
    wait.go               — Wait strategies
    websocket.go          — Service worker + websocket helpers
  network/
    interceptor.go        — CDP network interception (~606 lines)
    interceptor_test.go   — Interceptor tests
  pool/
    bufferpool.go         — Buffer pool for panic recovery
  queue/
    interface.go          — Queue interface
    local.go              — In-memory + persistent queue
    redis.go              — Redis queue backend
    postgres.go           — PostgreSQL queue backend
    kafka.go              — Kafka queue backend
    bloom.go              — Bloom filter dedup
    heap.go               — Priority heap implementation
    checkpoint.go         — Checkpoint save/load
  ratelimit/
    ratelimit.go           — Per-host token bucket rate limiter
  resilience/
    circuitbreaker.go     — Per-host circuit breaker (3-state)
    circuitbreaker_test.go
  rewrite/
    html.go               — HTML/CSS URL rewriter (~1151 lines)
    html_test.go          — Rewriter tests
  robots/
    robots.go             — robots.txt parser
    robots_test.go        — Robots tests
  storage/
    filesystem.go         — Filesystem output writer
    warc.go               — WARC archive writer
    resource_cache.go     — Incremental crawl cache
    filesystem_test.go    — Filesystem tests
    warc_test.go          — WARC tests
  util/
    util.go               — Logging, Metrics, BoundedQueue, LRUSet, MemoryBudget
```
