# Architecture

## Overview

Web Cloner (clone) is a production-grade browser-driven web archiver written in Go 1.25. It uses [chromedp](https://github.com/chromedp/chromedp) (Chrome DevTools Protocol) to crawl, render, and reconstruct JavaScript-heavy sites as static offline archives with WARC/WACZ output, change detection, and API/WebSocket capture.

## Design Principles

1. **Resilience over performance** - Graceful degradation, automatic recovery, fault isolation
2. **Observability** - Structured logging, metrics, tracing
3. **Extensibility** - Pluggable queue backends, modular architecture
4. **Security** - Anti-fingerprinting, secure defaults, credential handling
5. **Maintainability** - Clean interfaces, comprehensive tests, documentation

## System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Crawler Core                             │
│  ┌──────────────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐  │
│  │   Browser Pool   │  │  Queue   │  │   Rate   │  │Circuit │  │
│  │  ┌─Chrome 1───┐  │  │(Redis/   │  │ Limiter  │  │Breaker │  │
│  │  │ Tabs ...   │  │  │   PG/K)  │  │ per-host │  │per-host│  │
│  │  ├─Chrome 2───┤  │  │          │  │          │  │        │  │
│  │  │ Tabs ...   │  │  └──────────┘  └──────────┘  └────────┘  │
│  │  ├─Chrome N───┤  │                                          │
│  │  │ Tabs ...   │  │  ┌──────────────────────────────────┐    │
│  │  └────────────┘  │  │      Stealth / Interaction       │    │
│  └──────────────────┘  │  Canvas/WebRTC/font protection   │    │
│           │            │  Form filling + link clicking    │    │
│           ▼            └──────────────────────────────────┘    │
│  ┌──────────────────┐  ┌────────────┐  ┌──────────────────┐   │
│  │  Network Intercpt│  │   Rewrite  │  │    Storage       │   │
│  │  (XHR/fetch/WS)  │  │  (HTML/CSS │  │  FS / WARC/WACZ  │   │
│  │  Blocked URLs    │  │   /JS/Img) │  │  Screenshots/PDF │   │
│  └──────────────────┘  └────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         ▲                    ▲                    ▲
         │                    │                    │
┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐
│  Web UI      │  │  REST API    │  │  Notifications      │
│  Dashboard   │  │  Start/Stop  │  │  Webhook/Slack/SMTP │
└──────────────┘  └──────────────┘  └─────────────────────┘
```

## Core Components

### Browser Pool (`internal/browserpool/`)
- Manages N Chrome processes with LRU tab assignment
- Health checks and auto-restart on crash
- Supports remote Chrome via WebSocket
- Persistent profiles via `--user-data-dir`

### URL Queue (`internal/queue/`)
- Pluggable backends: in-memory, Redis, PostgreSQL, Kafka
- Priority queue (min-heap by depth) for BFS crawling
- Checkpoint/resume with atomic snapshots
- Bloom filter + LRU exact dedup

### Rate Limiter (`internal/ratelimit/`)
- Token bucket per host
- Dynamic capacity from robots.txt `Crawl-Delay`
- Adaptive rate adjustment based on observed latency

### Circuit Breaker (`internal/resilience/`)
- 3-state (closed/open/half-open) per host
- Configurable failure/success thresholds
- Automatic recovery probing

### Network Interceptor (`internal/network/`)
- CDP event capture (RequestWillBeSent, ResponseReceived, LoadingFinished)
- XHR/fetch/WebSocket capture with full bodies
- GraphQL operation detection
- HTTP fallback for missing CDP resources

### Rewrite Engine (`internal/rewrite/`)
- HTML tokenizer-based URL rewriting
- CSS `url()`, `@import`, `srcset` handling
- Shadow DOM serialization
- Relative path computation

### Storage (`internal/storage/`)
- Filesystem: per-host directory structure
- WARC 1.0 with gzip compression, 1GB rotation
- WACZ: ZIP with WARC + CDX index + metadata
- Incremental: ETag/Last-Modified caching

### Coordinator (`internal/coordinator/`)
- Distributed worker coordination
- Backends: none (standalone), file, Redis
- Leader election, worker registration, distributed locks

## Data Flow

```
Seed URLs
    │
    ▼
┌─────────────┐
│  Queue      │───► Priority by depth (BFS)
│  (Heap)     │
└─────────────┘
    │
    ▼
┌─────────────┐
│  Dispatcher │───► Rate Limiter (per-host)
│  (Workers)  │     Circuit Breaker (per-host)
└─────────────┘
    │
    ▼
┌─────────────┐
│  Browser    │───► Acquire tab from pool
│  Pool       │     Inject stealth scripts
└─────────────┘
    │
    ▼
┌─────────────┐
│  Navigation │───► Wait strategies (adaptive, networkidle, selector)
│  & Wait     │
└─────────────┘
    │
    ▼
┌─────────────┐
│  Interact   │───► Click, form fill, lazy load, infinite scroll
│  Engine     │     SPA route discovery
└─────────────┘
    │
    ▼
┌─────────────┐
│  Capture    │───► HTML (with Shadow DOM), screenshots, PDF
│  Pipeline   │     SingleFile, Article, Structured Data
└─────────────┘
    │
    ▼
┌─────────────┐
│  Network    │───► XHR/fetch/WS capture, API responses
│  Intercept  │
└─────────────┘
    │
    ▼
┌─────────────┐
│  Asset      │───► Download CSS/JS/images/fonts
│  Download   │     Content-hash dedup (XXHash + Bloom)
└─────────────┘
    │
    ▼
┌─────────────┐
│  Rewrite    │───► Replace URLs with local paths
│  & Save     │     Write to FS/WARC/WACZ
└─────────────┘
    │
    ▼
┌─────────────┐
│  Link       │───► Extract links, queue new URLs
│  Extraction │
└─────────────┘
```

## Concurrency Model

- **Browser Pool**: N Chrome processes, each handling multiple tabs
- **Page Workers**: Semaphore-limited concurrent page crawls (default 5)
- **Asset Workers**: Higher concurrency for asset downloads (default 16)
- **Network Intercept**: Worker pool for body fetching (configurable)
- **Queue Operations**: Mutex-protected or distributed (Redis/PG/Kafka)

## Configuration

All configuration via `internal/config/Config` struct:
- CLI flags override config file (JSON)
- 25+ validation rules
- Environment-specific defaults

## Security Considerations

- Anti-fingerprinting: Canvas noise, WebRTC masking, navigator overrides
- Credential handling: 0600 file permissions for cookies
- TLS verification configurable (default: enabled)
- Input validation on all user-provided URLs/patterns
- No eval/exec of untrusted code

## Deployment

- Single binary (static, ~37MB)
- Docker multi-stage build
- docker-compose with Chrome sidecar
- Kubernetes/Helm (planned)
- Distributed via Redis/PostgreSQL/Kafka queue backends

## Observability

- Structured JSON logging (zap)
- Prometheus metrics at `/metrics`
- REST API for status/control
- Web UI dashboard
- Webhook/Slack/Email notifications

## Testing

- Unit tests: `go test ./...`
- Race detector: `go test -race ./...`
- Integration tests with mock servers (planned)
- Fuzz testing for parsers (planned)

## Future Work

- Plugin system (WASM-based)
- LLM-optimized Markdown output
- Git-native storage backend
- Playwright/Puppeteer backend
- ML-based CAPTCHA solving
- Accessibility tree capture