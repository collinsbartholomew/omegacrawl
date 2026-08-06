# Testing

## Overview

Web Cloner uses a multi-layered testing strategy to ensure reliability and correctness.

## Test Types

### Unit Tests

Located alongside source files as `*_test.go`.

```bash
# Run all unit tests
go test ./...

# Run with race detector
go test -race ./...

# Run specific package
go test ./internal/crawler/...

# Run specific test
go test ./internal/crawler/ -run TestExtractGraphQLOp -v
```

**Coverage targets:**
- Core packages: >80%
- Utility packages: >90%
- New code: >85%

### Integration Tests

Integration tests use test servers and real Chrome instances.

```bash
# Requires Chrome/Chromium installed
go test -tags=integration ./internal/crawler/...
```

### Fuzz Testing

Fuzz tests for parsers and URL handling.

```bash
# Run fuzz tests (requires Go 1.18+)
go test -fuzz=FuzzNormalize ./internal/queue/...
go test -fuzz=FuzzRewrite ./internal/rewrite/...
```

### Property-Based Testing

Uses `testing/quick` for property verification.

```bash
go test ./internal/rewrite/ -run TestRewriteProperties -v
```

## Test Infrastructure

### Test Servers

- `httptest.Server` for HTTP endpoints
- Custom HTML fixtures for parsing tests
- Mock CDP responses for network intercept tests

### Fixtures

```
testdata/
├── html/
│   ├── simple.html
│   ├── shadow_dom.html
│   ├── spa_routes.html
│   └── infinite_scroll.html
├── css/
│   ├── imports.css
│   ├── fonts.css
│   └── sprites.css
├── har/
│   └── sample.har
└── warc/
    └── sample.warc
```

### Mock Objects

- `MockBrowserPool` for testing without Chrome
- `MockQueue` for deterministic queue behavior
- `MockStorage` for testing without filesystem I/O
- `MockNetworkInterceptor` for network capture tests

## Running Tests

### CI Pipeline

```yaml
# .github/workflows/test.yml
test:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.25'
    - name: Install Chrome
      run: sudo apt-get update && sudo apt-get install -y chromium
    - name: Run tests
      run: go test -race ./...
    - name: Run integration tests
      run: go test -tags=integration ./internal/crawler/...
```

### Local Development

```bash
# Quick test
go test ./...

# Verbose with race detector
go test -race -v ./internal/crawler/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Benchmarks
go test -bench=. -benchmem ./internal/queue/...
```

## Test Categories

### Crawler Tests (`internal/crawler/`)

| Test | Description |
|------|-------------|
| `TestStartRejectsAlreadyRunning` | Prevents duplicate crawl start |
| `TestStartRejectsInvalidSeeds` | Validates seed URLs |
| `TestIsSeedPage` | Seed page identification |
| `TestWriteSitemap` | Sitemap XML generation |
| `TestExtractGraphQLOp` | GraphQL operation extraction |
| `TestWriteSiteAggregate` | Per-host aggregate writes |
| `TestStartResetsPauseState` | Pause/resume state management |
| `TestStatusRunningReflectsStarter` | Status API accuracy |

### Queue Tests (`internal/queue/`)

| Test | Description |
|------|-------------|
| `TestPriorityQueue` | Heap operations, ordering |
| `TestRedisQueue` | Redis backend (requires Redis) |
| `TestPostgresQueue` | PG backend (requires PostgreSQL) |
| `TestKafkaQueue` | Kafka backend (requires Kafka) |
| `TestBloomFilter` | False positive rate, persistence |
| `TestQueueRace` | Concurrent access safety |

### Rewrite Tests (`internal/rewrite/`)

| Test | Description |
|------|-------------|
| `TestRewriteHTML` | HTML token rewriting |
| `TestRewriteCSS` | CSS url()/@import rewriting |
| `TestExtractLinks` | Link extraction accuracy |
| `TestExtractFonts` | @font-face URL extraction |
| `TestSingleFile` | Self-contained HTML generation |

### Network Tests (`internal/network/`)

| Test | Description |
|------|-------------|
| `TestInterceptor` | CDP event capture |
| `TestAPIResponse` | API response classification |
| `TestMissingResources` | HTTP fallback behavior |

### Auth Tests (`internal/auth/`)

| Test | Description |
|------|-------------|
| `TestFormLogin` | Form-based authentication |
| `TestBasicAuth` | HTTP Basic auth headers |
| `TestOAuthFlow` | Client credentials flow |
| `TestTokenRefresh` | OAuth token refresh |

### Storage Tests (`internal/storage/`)

| Test | Description |
|------|-------------|
| `TestFilesystem` | Path mapping, saving |
| `TestWARC` | WARC format compliance |
| `TestWACZ` | ZIP structure, CDX index |
| `TestIncremental` | ETag/Last-Modified caching |

## Test Patterns

### Table-Driven Tests

```go
func TestNormalizeURL(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"simple", "http://example.com", "http://example.com/"},
        {"with_path", "http://example.com/path", "http://example.com/path"},
        {"trailing_slash", "http://example.com/", "http://example.com/"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := NormalizeURL(tt.input)
            if result != tt.expected {
                t.Errorf("NormalizeURL(%q) = %q, want %q", tt.input, result, tt.expected)
            }
        })
    }
}
```

### Golden File Tests

```go
func TestRewriteHTML(t *testing.T) {
    input, _ := os.ReadFile("testdata/html/input.html")
    expected, _ := os.ReadFile("testdata/html/expected.html")
    
    result := RewriteHTML(input, "output/site/page.html")
    
    if !bytes.Equal(result, expected) {
        // Update golden file: go test -update
        t.Errorf("rewrite mismatch")
    }
}
```

### Parallel Tests

```go
func TestConcurrentQueue(t *testing.T) {
    t.Parallel()
    // ...
}
```

## Debugging Tests

### Verbose Output

```bash
go test -v ./internal/crawler/ -run TestStartRejectsAlreadyRunning
```

### Race Detector

```bash
go test -race ./...
```

### Single Test with Delve

```bash
dlv test ./internal/crawler/ -run TestStartRejectsAlreadyRunning
```

### Test Coverage

```bash
go test -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | grep -E "(crawler|queue|rewrite)"
go tool cover -html=coverage.out -o coverage.html
```

## Continuous Integration

### Pre-commit Hooks

```bash
# .pre-commit-config.yaml
repos:
  - repo: local
    hooks:
      - id: go-fmt
        name: go fmt
        entry: go fmt ./...
        language: system
      - id: go-vet
        name: go vet
        entry: go vet ./...
        language: system
      - id: go-test
        name: go test
        entry: go test ./...
        language: system
```

### Required Checks

- [ ] All tests pass
- [ ] No race conditions (`-race`)
- [ ] No vet warnings
- [ ] Code formatted (`gofmt`)
- [ ] Coverage >80% for modified packages
- [ ] No new lint warnings

## Test Data Management

### Generating Fixtures

```bash
# Capture real site for regression testing
./clone -d 1 --output testdata/sites/example https://example.com
```

### Updating Golden Files

```bash
# Update all golden files
go test -update ./internal/rewrite/...

# Update specific test
go test -update -run TestRewriteHTML ./internal/rewrite/
```

## Performance Testing

### Benchmarks

```bash
# Run benchmarks
go test -bench=. -benchmem ./internal/queue/...

# Profile CPU
go test -bench=. -cpuprofile=cpu.prof ./internal/queue/...
go tool pprof cpu.prof

# Profile Memory
go test -bench=. -memprofile=mem.prof ./internal/queue/...
go tool pprof mem.prof
```

### Load Testing

```bash
# Simulate concurrent crawls
for i in {1..10}; do
  ./clone -d 1 -o "loadtest/$i" https://example.com &
done
wait
```

## Troubleshooting

### Flaky Tests

- Use `t.Parallel()` for independent tests
- Avoid shared global state
- Use `-count=1` to disable caching

### Chrome Not Found

```bash
# Install Chrome
sudo apt-get install chromium
# or
brew install --cask chromium
```

### Timeout Issues

```bash
# Increase test timeout
go test -timeout 5m ./...
```

### Race Detector False Positives

```go
// Use sync/atomic or proper locking
var counter atomic.Int64
counter.Add(1)
```