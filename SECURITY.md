# Security

## Threat Model

Web Cloner is designed to crawl and archive web content. The primary security concerns are:

1. **Server-side attacks** - Malicious websites attempting to exploit the crawler
2. **Credential exposure** - Authentication cookies, tokens, API keys
3. **Resource exhaustion** - DoS via infinite loops, large responses, deep recursion
4. **Data integrity** - Tampering with archived content

## Security Features

### Anti-Fingerprinting (Stealth Mode)

Enabled by default (`--stealth`). Injects JavaScript to mask automation indicators:

| Technique | Implementation |
|-----------|----------------|
| Canvas noise | Deterministic pixel modification on `getImageData`/`toDataURL` |
| WebRTC masking | SDP rewriting to hide ICE candidate addresses |
| navigator.webdriver | Overridden to `undefined` |
| navigator.plugins | Mocked with realistic plugin list |
| navigator.languages | Fixed to `['en-US', 'en']` |
| Chrome runtime | Full mock object |
| WebGL vendor | Spoofed to Intel/Intel Iris |
| Hardware concurrency | Fixed to 8 |
| Device memory | Fixed to 8GB |
| Screen properties | Fixed to 1920×1080 |
| AudioContext | Normalized sine wave oscillator |

### Credential Handling

- Cookies stored with `0600` permissions (owner read/write only)
- OAuth tokens in memory only, never logged
- HTTP Basic auth credentials not persisted to disk
- Config file supports external secret references (planned)

### Input Validation

- All user-provided URLs validated with `net/url`
- Regex patterns for blocked URLs sanitized
- CSS selectors validated before injection
- File paths normalized and contained within output directory

### TLS

- Certificate verification enabled by default
- `--disable-tls-verify` flag for testing only (logs warning)
- Custom CA bundle support via environment (planned)

### Resource Limits

| Resource | Limit | Configurable |
|----------|-------|--------------|
| Max concurrent pages | 5 | `--concurrency` |
| Max asset concurrency | 16 | `AssetConcurrency` |
| Page timeout | 120s | `--timeout` |
| Max response body | 50MB | `MaxResponseBodySize` |
| Max queue size | 100,000 | `MaxTotalURLs` |
| Max URLs per host | 10,000 | `--max-urls` |
| Max depth | 10 | `--depth` |
| Memory budget | Unlimited | `MemoryBudget` |

### Network Security

- Proxy support (HTTP/HTTPS/SOCKS5)
- Blocked URL patterns for ads/trackers
- Per-host rate limiting (token bucket)
- Circuit breaker prevents hammering failing hosts
- robots.txt compliance (default: enabled)

## Secure Deployment

### Docker

```dockerfile
# Multi-stage build with minimal runtime image
FROM golang:1.25-alpine AS builder
# ... build ...
FROM alpine:3.20
RUN apk add --no-cache chromium libstdc++ ca-certificates tzdata
RUN adduser -D -h /home/cloner cloner
USER cloner
```

- Non-root user (`cloner`)
- Read-only root filesystem (recommended)
- Drop all capabilities
- Seccomp profile (recommended)

### Kubernetes

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
```

### Secrets Management

- Use Kubernetes secrets or external secret stores
- Mount secrets as files, not environment variables
- Rotate credentials regularly

## Vulnerability Reporting

Report security vulnerabilities to: security@example.com

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We aim to respond within 48 hours and patch critical issues within 7 days.

## Compliance

- No telemetry or data collection
- GDPR: No personal data stored unless explicitly crawled
- SOC 2: Auditable logging, access controls
- PCI DSS: No payment data processing

## Hardening Checklist

- [ ] Run as non-root user
- [ ] Enable TLS verification
- [ ] Use blocked URL patterns for untrusted sites
- [ ] Set resource limits (CPU, memory, disk)
- [ ] Monitor logs for anomalies
- [ ] Rotate credentials regularly
- [ ] Keep Chrome/Chromium updated
- [ ] Use read-only filesystem where possible
- [ ] Enable seccomp/AppArmor profiles
- [ ] Network policies to restrict egress