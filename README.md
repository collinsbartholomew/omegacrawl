# Web Cloner

Production-grade browser-driven web archiver. Drives Chrome via [chromedp](https://github.com/chromedp/chromedp) to crawl, render, and reconstruct JavaScript-heavy sites as static offline archives with WARC/WACZ output, change detection, and API/WebSocket capture.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

---

## Features

### Browser Engine
- **Headless Chrome** via chromedp — renders JS SPAs, handles redirects, scrolls infinite pages
- **Multi-browser pool** — N concurrent Chrome processes with LRU page assignment, health checks, and auto-restart on crash
- **Remote Chrome** — connect to existing Chrome instances via WebSocket (Docker, cloud, etc.)
- **Configurable flags** — add arbitrary `--chrome-flag` values from CLI or config
- **Persistent profiles** — `--user-data-dir` preserves sessions, cookies, localStorage between restarts

### Page Capture
- Full HTML with Shadow DOM serialization
- CSS/JS/image/font asset capture with URL rewriting for offline replay
- Screenshots (full-page) and PDF generation
- [SingleFile](https://github.com/gildas-lormeau/SingleFile)-style self-contained snapshots
- Article extraction (readability algorithm)
- Structured data extraction (JSON-LD, microdata, RDFa)

### Network Interception
- XHR/fetch/WebSocket capture with full request/response bodies
- GraphQL operation detection
- API response capture (filterable by URL pattern)
- Network request blocking (`--blocked-urls` filters ads, analytics, etc.)
- Configurable resource fallback for CDP-captured content

### Navigation & Waiting
- Multiple wait strategies: `domcontentloaded`, `networkidle`, `selector`, `adaptive`
- Configurable navigation timeout and quiet period
- Overlay dismissal and section expansion
- Custom JS injection before/after page load
- Clickable element interaction (CSS selectors)

### Interaction Engine
- Systematic link clicking and form filling
- Form field type detection (text, email, password, tel, URL, checkbox, radio, select, textarea)
- Context-aware values (names, emails, phones, search queries)
- Form submission detection (button click, JavaScript submit)

### Authentication
- Form-based login (configurable selectors)
- HTTP Basic Authentication
- Custom header injection
- OAuth 2.0 flows with token refresh support
- Pre-crawl interactive login (solve CAPTCHAs, 2FA, SSO manually)

### CAPTCHA Handling
- Automated solving: 2Captcha, AntiCaptcha, CapMonster
- Interactive mode: visible browser window, you solve challenges manually

### Output Formats
| Format | Description | Flag |
|---|---|---|
| **HTML/CSS/JS/Assets** | Static site reconstruction (default) | enabled by default |
| **WARC** | Standard web archive format | `--warc` |
| **WACZ** | Packaged web archive (ZIP with CDX index + metadata) | `--wacz` |
| **Screenshots** | Full-page PNG screenshots | `--screenshot` |
| **PDF** | Page PDF exports | `--pdf` |
| **API Responses** | JSON dumps of captured API traffic | `--intercept-apis` |

### Resilience
- **Multi-browser pool** — no single point of failure; crashed Chrome instances restart automatically
- Per-host rate limiting (token bucket, respects robots.txt `Crawl-Delay`)
- Per-host circuit breaker (3-state: closed/open/half-open)
- Retry with exponential backoff (uses proper error classification)
- Checkpoint/resume — saves queue state, survives process restarts
- Bloom filter dedup (LRU + bloom for URL deduplication)
- Graceful Chrome shutdown with timeout

### Infrastructure
- **Distributed queue backends:** in-memory, Redis, PostgreSQL, Kafka
- **Docker:** multi-stage Dockerfile + docker-compose with remote Chrome
- **REST API:** control crawls programmatically (start, stop, status)
- **Dashboard:** real-time web UI at `--dashboard-port`
- **Scheduling:** cron expression support for recurring crawls
- **Notifications:** webhook, Slack, email (SMTP) on completion/errors
- **Change detection:** snapshot-diff across crawls, HTML structural reports

---

## Quick Start

```bash
# Build
go build -o clone ./cmd/clone

# Basic crawl
./clone -d 1 https://example.com

# Interactive (visible browser, solve CAPTCHAs manually)
./clone --interactive https://example.com

# With screenshots and WACZ output
./clone -s --wacz https://example.com

# Multi-browser pool (3 Chrome processes)
./clone --browser-pool-size 3 https://example.com

# Remote Chrome (Docker)
docker compose up -d chrome
./clone --remote-chrome-url=ws://localhost:9222/devtools/browser/0 https://example.com

# Dashboard
./clone --dashboard-port 8080 https://example.com
# Open http://localhost:8080 in your browser
```

Default output goes to `./output/`. Use `-o` or `--output` to set a custom directory.

### Docker

```bash
# Full stack with Chrome container
docker compose up --build

# Or build and run standalone
docker build -t web-cloner .
docker run --rm -v ./output:/data/output web-cloner -d 1 https://example.com
```

---

## CLI Reference

```
Usage: clone [flags] URL...

Flags:
  -c, --config string           config file path (JSON)
  -d, --depth int               max crawl depth (default 10)
  -n, --concurrency int         max concurrent pages (default 5)
  -o, --output string           output directory (default "output")
  -s, --screenshot              take screenshots
  -p, --pdf                     generate PDFs
  -l, --log-level string        log level: debug, info, warn, error (default "info")
      --timeout duration        per-page timeout (default 120s)
      --delay duration          delay between requests to same host (default 1s)
      --proxy string            HTTP proxy for Chrome
      --stealth                 anti-bot stealth mode (default true)
      --no-robots               ignore robots.txt
      --max-urls int            max URLs per host (default 10000)
      --scroll                  infinite scroll detection (default true)
      --interact                enable interaction engine (click links, fill forms)
      --interactive             visible browser for manual CAPTCHA/form solving
      --manual-capture          user navigates freely, each page is captured
      --warc                    write WARC archive
      --wacz                    write WACZ packaged archive
      --chrome-flag strings     additional Chrome CLI flags (repeatable)
      --remote-chrome-url string  websocket URL for remote Chrome
      --browser-pool-size int   concurrent browser processes (default 1)
      --user-data-dir string    Chrome user data directory
      --blocked-urls strings    URL patterns to block (e.g. *doubleclick*)
      --dashboard-port int      web dashboard port (0 = disabled)
      --api-port int            REST API port (0 = disabled)
      --webhook-url string      notification webhook URL
      --slack-url string        Slack webhook URL
      --schedule string         cron expression (e.g. "0 6 * * *" or "@every 24h")

Subcommands:
  serve [directory]     Serve cloned output for local replay
  repair [directory]    Download missing assets in an existing clone and re-rewrite pages
  localize [directory]  Copy a clone into <dir>/localized with all refs rewritten to local files
  dedupe [directory]    Export unique pages/assets, collapsing duplicate/permutation pages
```

---

## Configuration

All features can be configured via JSON config file (`-c config.json`). CLI flags override file values.

### Full Config Schema

See [internal/config/config.go](internal/config/config.go) for the complete struct with all fields, defaults, and validation.

### Examples

**CAPTCHA automation (headless):**
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

**Multi-browser pool with remote Chrome:**
```json
{
  "seeds": ["https://example.com"],
  "browser_pool_size": 4,
  "remote_chrome_url": "ws://chrome:9222/devtools/browser/0",
  "max_concurrent_pages": 20
}
```

**Scheduled crawl with notifications:**
```json
{
  "seeds": ["https://example.com"],
  "schedule_cron": "0 6 * * *",
  "slack_url": "https://hooks.slack.com/services/...",
  "webhook_url": "https://myapp.com/webhook",
  "smtp": {
    "host": "smtp.gmail.com",
    "port": 587,
    "user": "you@gmail.com",
    "pass": "app-password",
    "from": "crawler@example.com",
    "to": ["admin@example.com"]
  }
}
```

**Network blocking + authentication:**
```json
{
  "seeds": ["https://example.com/dashboard"],
  "blocked_url_patterns": ["*doubleclick*", "*facebook*", "*analytics*"],
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

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          Crawler                                │
│  ┌──────────────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │   Browser Pool   │  │  Queue   │  │   Rate   │  │Circuit │ │
│  │  ┌─Chrome 1───┐  │  │  (Redis/ │  │  Limiter │  │Breaker │ │
│  │  │ Tabs ...   │  │  │   PG/K)  │  │ per-host │  │per-host│ │
│  │  ├─Chrome 2───┤  │  │          │  │          │  │        │ │
│  │  │ Tabs ...   │  │  └──────────┘  └──────────┘  └────────┘ │
│  │  ├─Chrome N───┤  │                                          │
│  │  │ Tabs ...   │  │  ┌──────────────────────────────────┐    │
│  │  └────────────┘  │  │      Stealth / Interaction       │    │
│  └──────────────────┘  │  Canvas/WebRTC/font protection   │    │
│           │            │  Form filling + link clicking    │    │
│           ▼            └──────────────────────────────────┘    │
│  ┌──────────────────┐  ┌────────────┐  ┌──────────────────┐  │
│  │  Network Intercpt│  │   Rewrite  │  │    Storage       │  │
│  │  (XHR/fetch/WS)  │  │  (HTML/CSS │  │  FS / WARC/WACZ  │  │
│  │  Blocked URLs    │  │   /JS/Img) │  │  Screenshots/PDF │  │
│  └──────────────────┘  └────────────┘  └──────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
         ▲                    ▲                    ▲
         │                    │                    │
  ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐
  │  Web UI      │  │  REST API    │  │  Notifications      │
  │  Dashboard   │  │  Start/Stop  │  │  Webhook/Slack/SMTP │
  └──────────────┘  └──────────────┘  └─────────────────────┘
```

### Key Design Decisions

- **Multi-browser pool** — N Chrome processes with LRU assignment. A crashed browser only loses its active pages; the pool auto-restarts it. Configure via `browser_pool_size`.
- **Per-page CDP tabs** — Each page gets its own chromedp tab context for isolation. The browser pool distributes page tabs across available Chrome processes.
- **Depth-first with priorities** — BFS-style URL queue (FIFO) ensures shallow pages are crawled before deep ones.
- **Token-bucket rate limiting** — Per-host with dynamic capacity from robots.txt crawl delay.
- **Externalized queue** — Pluggable backends (`local`, `redis`, `postgres`, `kafka`) for distributed crawling with checkpoint/resume.
- **Interactive mode** — Chrome runs with a visible window, concurrency locks to 1, and prompts block on stdin — letting you solve CAPTCHAs, fill forms, or handle 2FA in the live browser.

### Project Layout

```
cmd/clone/           CLI entrypoint (clone, serve, repair, localize, dedupe)
internal/
  api/               REST API server (start, stop, status, pause, resume, metrics)
  auth/              Form login, HTTP Basic, custom headers, OAuth 2.0
  browserpool/       Multi-browser process pool with health checks + auto-restart
  captcha/           2Captcha, AntiCaptcha, CapMonster integration
  changedetection/   Snapshot diffing across crawls
  config/            Configuration schema, defaults, validation
  crawler/           Core crawl loop split into focused files (page pipeline, capture,
                     interaction, output writers, resume, checkpoint, ...)
  errors/            Error classification (14 retryable/non-retryable types)
  httpclient/        Shared HTTP client pool
  jsanalyzer/        JavaScript dependency URL analysis
  jsengine/          Stealth, scroll, wait strategies, framework detection, SPA routes,
                     single-file, structured data, WebSocket capture
  localize/          Offline-localization pass + dedupe exporter (clone localize/dedupe)
  network/           CDP network interception (XHR/fetch/WebSocket)
  notify/            Notifications (webhook, Slack, SMTP email)
  pool/              Buffer pool for panic recovery
  queue/             URL queue backends (local, Redis, PostgreSQL, Kafka)
  ratelimit/         Token-bucket per-host rate limiter
  repair/            Missing-asset repair pass (clone repair)
  resilience/        Per-host circuit breaker (3-state)
  rewrite/           HTML/CSS/JS asset URL rewriting for offline replay
  robots/            robots.txt parser and sitemap crawler
  scheduler/         Cron-based crawl scheduler
  storage/           Filesystem, WARC, and WACZ output writers
  sync/              Synchronization primitives
  util/              Logging, metrics, LRU set, bounded queue, memory budget
  webui/             Real-time crawl dashboard (HTML + JSON API)
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

### Testing

```bash
# Unit tests
go test ./...

# Run specific tests
go test ./internal/queue/... -v -run TestPriorityQueue

# With race detection
go test -race ./...
```

---

## Docker

```bash
# Build
docker build -t web-cloner .

# Run (output in ./output)
docker run --rm -v ./output:/data/output web-cloner -d 1 https://example.com

# Full stack with docker-compose (Chrome container + cloner)
docker compose up --build
```

The `docker-compose.yml` pre-configures:
- A `chrome` service with remote debugging on port 9222
- A `cloner` service connected via `--remote-chrome-url`
- Volume-mapped output directory
- Configurable via `SEEDS` environment variable

---

## Output Structure

```
output/
├── clone/            # Raw crawl: host-mirrored pages, assets, and captures
│   ├── example.com/  #   HTML per page, CSS/JS/images/fonts, per-page article/structured data
│   ├── .mapping.json #   URL -> local-path mapping for the localize pass
│   └── index.json    #   URL -> path -> sha256 -> mime index
├── localized/        # Offline copy: every page/CSS rewritten to local files
├── dedup/            # Deduplicated export (clone dedupe) of unique pages + assets
└── .clone-state/     # checkpoint.bin + bloom.bin for resume
```

Per-page artifacts (article.json, structured-data.json, singlefile.html, shadowdom.json)
are stored next to each page's HTML file; the seed page also gets site-root copies for
downstream importers (e.g. the Next.js pipeline).

To replay locally:
```bash
./clone serve ./output
# Then open http://localhost:8080
```

---

## API

When `--api-port` is set, a REST API is available:

| Endpoint | Method | Description |
|---|---|---|
| `/api/status` | GET | Current crawl status (pages, errors, queue, elapsed, paused) |
| `/api/start` | POST | Start a new crawl (body: `{"seeds": ["..."]}`) |
| `/api/stop` | POST | Stop the current crawl |
| `/api/pause` | POST | Pause crawl (no new pages, finish active) |
| `/api/resume` | POST | Resume paused crawl |
| `/metrics` | GET | Prometheus metrics (if enabled) |

All endpoints return JSON. CORS headers are set for cross-origin access.

---

## License

MIT
