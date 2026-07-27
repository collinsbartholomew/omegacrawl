# Web Cloner

Production-grade browser-based web archiver. Drives headless Chrome via chromedp to crawl, capture, and reconstruct JavaScript-heavy sites as static HTML/CSS/image assets with WARC output, change detection, and API capture.

---

## Features

- **Real browser engine** — Chromedp-based, renders JS SPAs, handles redirects, scrolls infinite pages
- **CAPTCHA handling** — Automated solving via 2Captcha/AntiCaptcha/CapMonster, or **interactive mode** with a visible browser for manual solving and form filling
- **Authentication** — Form login, HTTP Basic, custom headers, OAuth 2.0 flows
- **Network interception** — Captures XHR/fetch/WebSocket traffic, GraphQL operations, and API payloads
- **Output formats** — Static HTML files, WARC archives, screenshots, PDFs, SingleFile snapshots
- **Resilience** — Per-host rate limiting, circuit breakers, retry with backoff, checkpoint/resume
- **robots.txt** — Respects crawl directives, caches sitemap-discovered URLs
- **Change detection** — Compare snapshots across crawls, report structural differences
- **Queue backends** — In-memory, Redis, PostgreSQL, Kafka

---

## Quick Start

```bash
go build -o clone ./cmd/clone
./clone -d 1 https://example.com
```

Default output goes to `./output/`. Use `--output` to set a custom directory.

### CLI

```
Usage: clone [options] <seed URLs...>

Options:
  -c, --config string       config file path (JSON)
  -d, --depth int           max crawl depth (default 2)
  -o, --output string       output directory (default "./output")
  -p, --pages int           max concurrent pages (default 3)
  -t, --timeout duration    per-page timeout (default 30s)
  -s, --stealth             enable stealth mode (hide automation)
  -l, --log string          log level: debug, info, warn, error (default "info")
  -u, --user-agent string   custom user agent
  --proxy string            HTTP proxy for Chrome
  --interactive             show browser, solve CAPTCHAs/challenges manually
```

---

## Usage

### Basic crawl

```bash
./clone https://example.com
```

### Config file

```json
{
  "seeds": ["https://example.com"],
  "max_depth": 3,
  "max_concurrent_pages": 5,
  "output_dir": "./archive",
  "respect_robots": true,
  "stealth": true,
  "screenshot": true
}
```

```bash
./clone -c config.json
```

### Interactive mode

Launches a visible Chrome window and pauses on each page so you can solve CAPTCHAs, fill forms, or handle challenges manually. Prompts for Enter before capturing content.

```bash
./clone --interactive https://example.com/login
```

Or via config:

```json
{
  "interactive": true,
  "seeds": ["https://example.com"]
}
```

**Pre-crawl login** — When `auth.login_url` is set alongside `interactive: true`, the browser navigates to the login page before the crawl begins. Log in manually (handles CAPTCHAs, 2FA, SSO), press Enter, and the crawl continues authenticated via the captured cookies. Automated auth (form fill, OAuth) is skipped in interactive mode — you handle it directly.

```json
{
  "interactive": true,
  "auth": {
    "enabled": true,
    "type": "form",
    "login_url": "https://example.com/login"
  },
  "seeds": ["https://example.com/dashboard"]
}
```

### CAPTCHA automation (headless)

```json
{
  "captcha": {
    "enabled": true,
    "provider": "2captcha",
    "api_key": "...",
    "timeout": "120s",
    "retry_count": 3
  }
}
```

Supported providers: `2captcha`, `anticaptcha`, `capmonster`.

### Authentication

```json
{
  "auth": {
    "enabled": true,
    "type": "form",
    "login_url": "https://example.com/login",
    "username": "user",
    "password": "pass",
    "form_fields": {
      "#username": "user",
      "#password": "pass"
    },
    "submit_selector": "#login-btn",
    "wait_after_login": "5s"
  }
}
```

Types: `form`, `basic`, `header`, `oauth`.

---

## Configuration

| Field | Default | Description |
|---|---|---|
| `max_depth` | `2` | Maximum crawl depth |
| `max_concurrent_pages` | `3` | Parallel page limit |
| `page_timeout` | `30s` | Per-page timeout |
| `crawl_delay` | `1s` | Delay between requests to same host |
| `respect_robots` | `true` | Obey robots.txt directives |
| `interactive` | `false` | Visible browser, manual CAPTCHA/form solving |
| `stealth` | `false` | Hide Chrome automation indicators |
| `screenshot` | `false` | Capture page screenshots |
| `pdf` | `false` | Capture page PDFs |
| `warc` | `false` | Write WARC archive |
| `singlefile` | `false` | Save SingleFile snapshots |
| `article_extract` | `false` | Extract article content (readability) |
| `infinite_scroll` | `null` | Scroll-to-load configuration |
| `wait_strategy` | `"domcontentloaded"` | Page readiness: `domcontentloaded`, `networkidle`, `selector` |
| `max_urls_per_host` | `1000` | Cap URLs per host |
| `max_total_urls` | `100000` | Global URL cap |
| `intercept_apis` | `[]` | URL patterns to capture as API responses |
| `click_selectors` | `[]` | CSS selectors to click on each page |
| `enable_interaction_engine` | `false` | Automated link clicking and form filling |

Full schema in `internal/config/config.go`.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Crawler                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │  Chrome   │  │  Queue   │  │  Rate    │  │Circuit │ │
│  │  Pool     │  │  (Redis/ │  │  Limiter │  │Breaker │ │
│  │(1 alloc + │  │  PG/Kfk) │  │ per-host │  │per-host│ │
│  │ per-page  │  │          │  │          │  │        │ │
│  │  tabs)    │  └──────────┘  └──────────┘  └────────┘ │
│  └──────────┘                                          │
│           │                                            │
│           ▼                                            │
│  ┌──────────────────┐  ┌────────────┐  ┌────────────┐ │
│  │  Network Intercpt │  │   Rewrite  │  │  Storage   │ │
│  │  (XHR/fetch/WS)   │  │  (HTML/CSS │  │  (FS/WARC) │ │
│  │                   │  │   /JS/Img) │  │            │ │
│  └──────────────────┘  └────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### Key design decisions

- **Single browser, per-page tabs** — One Chrome process with `NewExecAllocator`; each page gets its own chromedp tab context for isolation without the overhead of multiple browser processes.
- **Depth-first with priorities** — BFS-style URL queue (FIFO) ensures shallow pages are crawled before deep ones; configurable `max_depth`.
- **Token-bucket rate limiting** — Per-host token bucket with dynamic capacity derived from robots.txt crawl delay; circuit breaker opens on error rate spikes.
- **Externalized queue** — Plugable backends (`local`, `redis`, `postgres`, `kafka`) for distributed crawling with checkpoint/resume.
- **Interactive mode** — When enabled, Chrome runs headless=false (visible window), concurrency locks to 1, and `promptUser()` blocks on stdin — letting you solve CAPTCHAs, fill forms, or handle 2FA in the live browser before content is captured.

---

## Output structure

```
output/
├── html/           # Stored per-path: example.com/path/to/page.html
├── css/            # Stylesheets (inlined + external)
├── js/             # JavaScript files
├── images/         # Images, favicons
├── fonts/          # Web fonts
├── api/            # Captured API responses (XHR/fetch/WS)
├── screenshots/    # Full-page screenshots (if enabled)
├── pdf/            # PDF exports (if enabled)
├── singlefile/     # SingleFile snapshots (if enabled)
├── warc/           # WARC archive (if enabled)
└── snapshots/      # Change detection snapshots (if enabled)
```

---

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

### Dependencies

- Go 1.25+
- Chrome or Chromium (for chromedp)
- Optional: Redis, PostgreSQL, or Kafka for distributed queue backends

### Project layout

```
cmd/clone/           # CLI entrypoint
internal/
  auth/              # Authentication (form, basic, header, OAuth)
  captcha/           # CAPTCHA solver (2Captcha, AntiCaptcha, CapMonster)
  changedetection/   # Snapshot diffing across crawls
  config/            # Configuration schema and validation
  crawler/           # Core crawl loop, browser management, page pipeline
  errors/            # Retryable error types
  httpclient/        # Shared HTTP client pool
  jsanalyzer/        # JavaScript analysis
  jsengine/          # Page interaction engine, scroll, stealth, framework detection
  network/           # CDP network interception (XHR/fetch/WebSocket)
  pool/              # Buffer pool
  queue/             # URL queue (local, Redis, PostgreSQL, Kafka)
  ratelimit/         # Token-bucket rate limiter
  resilience/        # Circuit breaker
  rewrite/           # HTML/CSS/JS rewriting for offline use
  robots/            # robots.txt parser and sitemap crawler
  storage/           # Filesystem and WARC output
  sync/              # Synchronization primitives
  util/              # Logging, metrics, LRU set
```

---

## License

MIT
