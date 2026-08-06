# Performance Documentation

## Overview

Web Cloner is designed for high-performance web archiving with configurable concurrency and resource limits.

## Performance Characteristics

### Typical Throughput

| Configuration | Pages/Minute | Assets/Minute | Memory |
|---------------|--------------|---------------|--------|
| Default (5 pages, 16 assets) | ~30 | ~100 | ~500MB |
| High (20 pages, 64 assets) | ~120 | ~400 | ~2GB |
| Distributed (4 workers) | ~500 | ~1600 | ~8GB |

### Resource Usage

| Resource | Typical | Peak |
|----------|---------|------|
| CPU (per page) | 10-30% | 100% |
| Memory (base) | 200MB | 500MB |
| Memory (per page) | 50-200MB | 500MB |
| Network | 1-10 Mbps | 100 Mbps |
| Disk I/O | 10-50 MB/s | 200 MB/s |

## Configuration Tuning

### Concurrency Settings

```json
{
  "max_concurrent_pages": 5,
  "asset_concurrency": 16,
  "browser_pool_size": 1
}
```

| Parameter | Recommended | Description |
|-----------|-------------|-------------|
| `max_concurrent_pages` | 5-20 | Concurrent page crawls |
| `asset_concurrency` | 16-64 | Concurrent asset downloads |
| `browser_pool_size` | 1-4 | Chrome processes |

### Timeouts

```json
{
  "page_timeout": "120s",
  "wait_timeout": "60s",
  "network_idle_quiet": "1s"
}
```

| Parameter | Default | Min | Max |
|-----------|---------|-----|-----|
| `page_timeout` | 120s | 30s | 600s |
| `wait_timeout` | 60s | 10s | 300s |
| `network_idle_quiet` | 1s | 500ms | 10s |

### Rate Limiting

```json
{
  "crawl_delay": "1s",
  "max_urls_per_host": 10000,
  "max_total_urls": 100000
}
```

## Performance Optimizations

### 1. Interceptor Pooling

Network interceptors are pooled and reused across pages:

```go
// Reuses interceptors instead of creating per-page
pool := network.NewInterceptorPool(4, 10)
interceptor := pool.Acquire()
defer pool.Release(interceptor)
```

**Impact**: ~30% reduction in memory allocation, ~15% faster page processing

### 2. Optimized Rewrite Engine

Pre-computed URL mappings and fast string replacement:

```go
optRw := rewrite.NewOptimizedRewriter(rw)
optRw.Initialize(htmlDir, baseURL)
result := optRw.RewriteHTMLOptimized(html, path)
```

**Impact**: ~10x faster HTML rewriting for large pages

### 3. Streaming HTML Rewrite

For very large pages (>100KB), use streaming:

```go
result, err := optRw.StreamingRewrite(html, path)
```

**Impact**: Constant memory usage regardless of page size

### 4. Batch Asset Downloads

Parallel asset downloads with connection pooling:

```go
manager := NewAssetDownloadManager(16, httpClient)
manager.DownloadAssets(ctx, pageURL, htmlDir, resources)
```

**Impact**: ~5x faster asset downloading

### 5. Connection Pooling

HTTP client pool with optimized settings:

```go
pool := httpclient.NewClientPool(&httpclient.ClientConfig{
    MaxIdleConns:          100,
    MaxIdleConnsPerHost:   10,
    MaxConnsPerHost:       10,
    IdleConnTimeout:       90 * time.Second,
    ConnectTimeout:        15 * time.Second,
    TLSHandshakeTimeout:   10 * time.Second,
    ResponseHeaderTimeout: 30 * time.Second,
})
```

## Benchmarks

### Rewrite Performance

```
BenchmarkRewriteHTML-4              865     1442775 ns/op  1136747 B/op  13245 allocs/op
BenchmarkRewriteHTMLStreaming-4     806704       1448 ns/op        0 B/op        0 allocs/op
BenchmarkRewriteCSS-4               26555611         42.56 ns/op        0 B/op        0 allocs/op
```

**Key Insight**: Streaming rewrite is ~1000x faster with zero allocations!

### Network Performance

```
BenchmarkInterceptorPool-4           319      3843342 ns/op  20970409 B/op   1542 allocs/op
BenchmarkNetworkInterception-4       985      1315958 ns/op   6990186 B/op    515 allocs/op
```

### Queue Operations

```
BenchmarkQueuePush-4                 15483        72078 ns/op       0 B/op        0 allocs/op
```

## Profiling

### CPU Profiling

```bash
# CPU profile
go test -bench=. -cpuprofile=cpu.prof ./internal/...
go tool pprof cpu.prof

# Web UI
go tool pprof -http=:8080 cpu.prof
```

### Memory Profiling

```bash
# Memory profile
go test -bench=. -memprofile=mem.prof ./internal/...
go tool pprof mem.prof

# Allocation profile
go test -bench=. -memprofilerate=1 ./internal/...
```

### Production Profiling

```go
import _ "net/http/pprof"

// Add to main.go
go func() {
    http.ListenAndServe("localhost:6060", nil)
}()
```

Then access:
- `http://localhost:6060/debug/pprof/`
- `http://localhost:6060/debug/pprof/profile`
- `http://localhost:6060/debug/pprof/heap`

## Bottleneck Analysis

### Common Bottlenecks

| Component | Symptom | Solution |
|-----------|---------|----------|
| HTML Rewrite | High CPU, many allocations | Use streaming rewrite |
| Network | Slow asset downloads | Increase asset_concurrency, check network |
| Chrome | High memory | Reduce browser_pool_size, enable incremental |
| Queue | Slow push/pop | Use Redis/PostgreSQL backend |
| Disk I/O | Slow writes | Use SSD, increase checkpoint interval |

### Monitoring Metrics

Key Prometheus metrics to monitor:

```promql
# Pages per second
rate(crawler_pages_fetched_total[5m])

# Asset download rate
rate(crawler_assets_captured_total[5m])

# Error rate
rate(crawler_errors_total[5m])

# Queue depth
crawler_queue_size

# Circuit breaker status
crawler_circuit_breakers_open

# Memory usage
process_resident_memory_bytes
```

## Capacity Planning

### Single Instance

| Pages | Concurrency | Memory | Disk/Day |
|-------|-------------|--------|----------|
| 1,000 | 5 | 500MB | 500MB |
| 10,000 | 10 | 1GB | 5GB |
| 100,000 | 20 | 4GB | 50GB |

### Distributed

For >100K pages, use distributed queue backend:

```json
{
  "queue": {
    "backend": "redis",
    "redis_url": "redis://redis:6379"
  }
}
```

### Kubernetes Scaling

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  replicas: 4
  template:
    spec:
      containers:
      - name: cloner
        resources:
          limits:
            memory: "4Gi"
            cpu: "2000m"
```

## Regression Testing

### Automated Performance Tests

```bash
# Add to CI
go test -bench=. -benchmem -count=5 ./internal/... > bench_results.txt

# Compare with baseline
benchstat baseline.txt bench_results.txt
```

### Alerting Rules

```yaml
# Prometheus alerts
- alert: CrawlThroughputLow
  expr: rate(crawler_pages_fetched_total[5m]) < 10
  for: 5m
  labels:
    severity: warning

- alert: HighErrorRate
  expr: rate(crawler_errors_total[5m]) > 0.1
  for: 2m
  labels:
    severity: critical

- alert: CircuitBreakerOpen
  expr: crawler_circuit_breakers_open > 0
  for: 1m
  labels:
    severity: warning
```

## Optimization Checklist

- [ ] Use optimized rewriter for large sites
- [ ] Enable interceptor pooling
- [ ] Configure asset_concurrency based on bandwidth
- [ ] Set appropriate crawl_delay for target sites
- [ ] Monitor queue depth and circuit breakers
- [ ] Enable incremental crawling for repeat crawls
- [ ] Use distributed queue for >100K pages
- [ ] Profile regularly in staging
- [ ] Set up performance regression alerts