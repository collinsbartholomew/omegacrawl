# ADR 001: Crawl vs. Localize Separation

## Status
Accepted

## Context
Web Cloner needs to handle JavaScript-heavy websites that require browser rendering. The traditional approach of inline rewriting during crawl has limitations: URLs in JavaScript bundles, dynamic imports, and framework-specific payloads (like Next.js `__NEXT_DATA__`) cannot be fully rewritten until all URLs are known.

## Decision
Separate the crawl phase from the localization phase:
1. **Crawl Phase**: Use headless browser to render pages, capture all network traffic, discover all URLs, and save raw content with URL-to-local-path mappings.
2. **Localize Phase**: Post-process the raw crawl output, using complete URL mapping to rewrite all references (HTML, CSS, JS) to local relative paths.

## Consequences
- **Pros**: Complete knowledge of all URLs enables accurate rewriting of JS bundles, dynamic imports, and framework payloads.
- **Cons**: Requires two passes; more disk space for raw + localized copies; crawl cannot be streamed directly to final output.

## Alternatives Considered
- Inline rewriting during crawl: Rejected due to inability to handle dynamic imports and framework payloads.
- Single-pass with speculative rewriting: Rejected due to high risk of broken links.

---

# ADR 002: Pluggable Queue Backends

## Status
Accepted

## Context
Different deployment scenarios require different queue characteristics: local development (in-memory), distributed crawling (Redis/PostgreSQL/Kafka), and high-throughput scenarios.

## Decision
Define a `queue.Queue` interface with multiple implementations:
- **Local**: In-memory min-heap + LRU + Bloom filter (default)
- **Redis**: Lua scripts for atomic operations
- **PostgreSQL**: `FOR UPDATE SKIP LOCKED` for atomic pops
- **Kafka**: Topic-based with local seen-set mirror

## Consequences
- **Pros**: Flexible deployment; can scale from laptop to cluster without code changes.
- **Cons**: Interface complexity; distributed backends need careful dedup handling.

---

# ADR 003: CDP over Puppeteer/Playwright

## Status
Accepted

## Context
Browser automation can use high-level libraries (Puppeteer, Playwright) or the Chrome DevTools Protocol (CDP) directly.

## Decision
Use `chromedp` (Go CDP client) directly instead of Puppeteer/Playwright.

## Consequences
- **Pros**: Native Go, no Node.js dependency, lower overhead, direct CDP access for advanced features (network interception, custom domains).
- **Cons**: More verbose API; manual handling of some high-level operations; smaller community than Puppeteer.

---

# ADR 004: WARC/WACZ over Custom Format

## Status
Accepted

## Context
Archive format choice affects long-term preservation, tooling compatibility, and interoperability.

## Decision
Use WARC 1.0 with gzip compression and WACZ (ZIP + WARC + CDX) as primary output formats.

## Consequences
- **Pros**: International standard (ISO 28500); supported by IIPC, WARC tools, replay systems (pywb, webrecorder); CDX index enables fast lookup.
- **Cons**: Verbose format; gzip compression overhead; WACZ spec still evolving.

---

# ADR 005: Post-Crawl Localize vs. Inline Rewrite

## Status
Accepted

## Context
URL rewriting can happen during crawl (inline) or after crawl (post-process).

## Decision
Post-crawl localization (see ADR 001).

## Consequences
- **Pros**: Complete URL map available; can rewrite JS bundles, `__NEXT_DATA__`, dynamic imports.
- **Cons**: Two-pass; requires storing raw + localized copies.

---

# ADR 006: Single-Process Chrome Pool

## Status
Accepted (for now)

## Context
Browser pool manages Chrome processes. Multiple processes provide isolation but increase memory.

## Decision
Single-process pool by default (`BrowserPoolSize=1`), with LRU tab assignment within the process.

## Consequences
- **Pros**: Lower memory; simpler process management; sufficient for most crawls.
- **Cons**: Single point of failure; no horizontal scaling within a single process.
- **Future**: Distributed Chrome cluster (ADR 009) for horizontal scaling.

---

# ADR 007: Dead Interfaces Removal

## Status
Accepted (completed)

## Context
`internal/crawler/interfaces.go` defined interfaces (`PageCrawler`, `AssetDownloader`, etc.) that were never instantiated.

## Decision
Remove unused interfaces and structs (`PageCrawler`, `AssetDownloader`, `LinkExtractor`, `CapturePipeline`, `InteractionEngine`, `SPAHandler`, `CrawlerConfig`, `RetryableError`, `jsonHeadrs`).

## Consequences
- **Pros**: Cleaner codebase; reduced cognitive load; no dead code.
- **Cons**: None (code was dead).

---

# ADR 008: JS Bundle Rewriting Layer

## Status
Accepted (implemented)

## Context
Next.js/React/Vue sites use webpack bundles with dynamic imports, `__NEXT_DATA__`, and framework-specific runtime config that break if not localized.

## Decision
Add `internal/localize/js.go` with `JSReWriter` that rewrites:
- `__webpack_require__.p` (public path)
- Dynamic `import()` calls
- Webpack chunk loading (`__webpack_require__.e`)
- Next.js runtime config (`assetPrefix`, `buildId`)
- `__NEXT_DATA__` JSON (assetPrefix, dynamicImports)
- Next.js image loader URLs
- Absolute URLs in JS string literals

## Consequences
- **Pros**: High-fidelity localization for Next.js/React/Vue.
- **Cons**: Regex-based (fragile); no source map support yet.

---

# ADR 009: Distributed Chrome Cluster (Proposed)

## Status
Proposed

## Context
Single-process Chrome pool limits horizontal scaling. Large crawls need multiple Chrome processes across machines.

## Decision
Implement distributed Chrome cluster using CDP multi-client support:
- Pool manager coordinates multiple `chromedp` allocators across machines
- Leader election for pool coordination
- Shared queue for tab distribution
- Health checks and automatic failover

## Consequences
- **Pros**: Horizontal scaling to 100+ concurrent pages.
- **Cons**: Complex distributed systems challenges; CDP session management across network.

---

# ADR 010: Mock API Generation (Proposed)

## Status
Proposed

## Context
Offline replay of SPAs requires mocking API responses captured during crawl.

## Decision
Generate Mock Service Worker (MSW) handlers + Express/Go mock server from captured network traffic:
- Parse captured XHR/Fetch/GraphQL/WebSocket traffic
- Generate OpenAPI 3.1 spec from traffic
- Generate MSW handlers for browser replay
- Generate Go/Express mock server for Node/Go replay

## Consequences
- **Pros**: True offline replay for SPAs; contract testing; SDK generation.
- **Cons**: Complex; requires handling auth, dynamic responses, streaming.