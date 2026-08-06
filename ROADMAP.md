# Roadmap

## Vision

Transform Web Cloner into the world's most capable, reliable, and extensible web archiving platform.

## Current Status: v1.1.0 (Stable)

✅ Core crawling with Chrome DevTools Protocol
✅ JavaScript rendering, SPA support, infinite scroll
✅ Shadow DOM, WebSocket, service worker handling
✅ CSS/HTML rewriting for offline replay
✅ Multi-format output (HTML, WARC, WACZ, screenshots, PDF)
✅ Progress reporting, rate limiting, circuit breakers
✅ REST API, Web UI dashboard
✅ Docker, Kubernetes deployment

---

## Phase 1: Production Hardening (Q1 2025) - **IN PROGRESS**

### Reliability
- [x] OpenTelemetry distributed tracing
- [x] Health check endpoints (/healthz, /readyz)
- [x] API rate limiting and CORS hardening
- [x] Configuration validation at startup
- [x] Secrets management (env vars, file references)
- [x] Audit logging with JSON Lines output
- [ ] Graceful shutdown with drain
- [ ] Checkpoint backup/restore
- [ ] Dead letter queue for failed URLs

### Observability
- [x] Prometheus metrics
- [x] Structured logging (zap)
- [x] Distributed tracing (OTLP/stdout)
- [ ] Custom dashboards (Grafana)
- [ ] Alerting rules (PrometheusRule)
- [ ] Log aggregation (Loki/ELK)

### Security
- [x] Anti-fingerprinting stealth mode
- [x] TLS verification (with override)
- [x] Rate limiting and circuit breakers
- [x] URL blocking patterns
- [x] Config sanitization for logging
- [ ] API authentication (API keys/JWT)
- [ ] RBAC for API endpoints
- [ ] Dependency scanning in CI

---

## Phase 2: Enhanced Capabilities (Q2 2025)

### Output Formats
- [x] LLM-optimized Markdown export
- [x] Accessibility tree capture (WCAG analysis)
- [x] Change detection with diff reports
- [ ] SingleFile HTML export
- [ ] Article extraction (readability)
- [ ] Structured data extraction (JSON-LD, microdata)
- [ ] WARC 1.1 with CDX index
- [ ] WACZ with signed metadata

### Browser Capabilities
- [ ] Multiple browser backends (Playwright, Firefox)
- [ ] Mobile emulation improvements
- [ ] Geolocation/timezone spoofing
- [ ] HTTP/3 and QUIC support
- [ ] WebAssembly execution
- [ ] IndexedDB/localStorage capture

### Interaction Engine
- [x] Form filling and submission
- [x] Link clicking with selectors
- [ ] Infinite scroll pagination handling
- [ ] Modal/dialog handling
- [ ] Infinite scroll with "load more" buttons
- [ ] Authentication flow automation (OAuth, SSO)
- [ ] CAPTCHA solving integration (2Captcha, etc.)

### Performance
- [x] Interceptor pooling
- [x] Optimized rewrite engine (10x faster)
- [x] Streaming HTML rewrite
- [x] Batch asset downloads with connection pooling
- [ ] Incremental crawling (ETag/Last-Modified)
- [ ] Distributed crawling with Redis/PostgreSQL
- [ ] GPU acceleration for screenshots
- [ ] Profile-guided optimization

---

## Phase 3: Extensibility & Ecosystem (Q3 2025)

### Plugin Architecture
- [x] WASM-based plugin system
- [ ] Plugin SDK (Go, Rust, JavaScript)
- [ ] Plugin marketplace
- [ ] Built-in plugins:
  - [ ] Screenshot comparison
  - [ ] SEO analysis
  - [ ] Performance metrics (Core Web Vitals)
  - [ ] Security headers analysis
  - [ ] Cookie consent detection
  - [ ] GDPR/CCPA compliance checks

### Integrations
- [ ] ArchiveBox integration
- [ ] SingleFile export
- [ ] Wget compatibility mode
- [ ] WARC tools compatibility
- [ ] IIIF manifest generation
- [ ] OAI-PMH harvesting
- [ ] S3/GCS/Azure Blob storage backends

### Developer Experience
- [ ] VS Code extension
- [ ] CLI autocomplete (bash/zsh/fish)
- [ ] Web-based config editor
- [ ] Crawl replay/debug UI
- [ ] Interactive rule builder
- [ ] Migration tooling (from wget, httrack, ArchiveBox)

---

## Phase 4: Enterprise & Scale (Q4 2025)

### Multi-tenancy
- [ ] Organization/workspace model
- [ ] Role-based access control
- [ ] Quota management
- [ ] Audit trail
- [ ] SSO integration (OIDC, SAML)

### Distributed Architecture
- [ ] Coordinator for multi-node crawls
- [ ] Workload distribution
- [ ] Shared cache layer
- [ ] Cross-region replication
- [ ] Auto-scaling policies

### Advanced Features
- [ ] AI-powered content extraction
- [ ] Semantic similarity detection
- [ ] Visual regression testing
- [ ] A/B crawl comparison
- [ ] Scheduled crawls with cron
- [ ] Webhook notifications
- [ ] Slack/Discord/Teams integration

### Compliance
- [ ] GDPR data subject requests
- [ ] SOC 2 Type II compliance
- [ ] FedRAMP readiness
- [ ] Digital preservation standards (PREMIS, METS)

---

## Phase 5: Research & Innovation (2026+)

### AI/ML Integration
- [ ] LLM-based content summarization
- [ ] Automatic classification/tagging
- [ ] Anomaly detection in crawl patterns
- [ ] Predictive crawl scheduling
- [ ] Natural language query interface

### Emerging Technologies
- [ ] WebAssembly for plugins
- [ ] WebGPU for rendering
- [ ] HTTP/3 and WebTransport
- [ ] Decentralized storage (IPFS, Filecoin)
- [ ] Blockchain notarization (WORM)

### Community
- [ ] Plugin marketplace
- [ ] Shared rule sets
- [ ] Collaborative crawl campaigns
- [ ] Public archive federation

---

## Release Cadence

| Release | Target | Focus |
|---------|--------|-------|
| v1.2.0 | Q1 2025 | Production hardening |
| v1.3.0 | Q2 2025 | Enhanced capabilities |
| v1.4.0 | Q3 2025 | Extensibility & ecosystem |
| v2.0.0 | Q4 2025 | Enterprise & scale |
| v3.0.0 | 2026 | AI/ML & innovation |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow, coding standards, and PR process.

## Feedback

- GitHub Issues: Bug reports, feature requests
- GitHub Discussions: Design discussions, questions
- Security: security@example.com

---

*Last updated: 2025-01-06*