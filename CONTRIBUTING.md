# Contributing to Web Cloner

Thank you for your interest in contributing to Web Cloner! This document provides guidelines for contributing to the project.

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please be respectful and constructive in all interactions.

## Getting Started

### Prerequisites

- Go 1.25+
- Chrome/Chromium (for integration tests)
- Git
- Make (optional)

### Development Setup

```bash
# Fork and clone
git clone https://github.com/your-username/web-cloner.git
cd web-cloner

# Install dependencies
go mod download

# Install development tools
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

# Run tests
go test ./...

# Build
go build -o web-cloner ./cmd/clone
```

## Development Workflow

### 1. Create an Issue

Before starting work, create an issue describing:
- The problem or feature request
- Proposed solution
- Any breaking changes

### 2. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/your-bug-fix
```

### 3. Make Changes

- Write clean, well-documented code
- Add tests for new functionality
- Update documentation as needed

### 4. Run Tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific package
go test ./internal/crawler/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 5. Code Quality

```bash
# Format
go fmt ./...

# Vet
go vet ./...

# Lint
golangci-lint run ./...

# Security scan
govulncheck ./...
```

### 6. Commit

```bash
git add .
git commit -m "feat: add amazing feature

- Add new functionality for X
- Fix edge case in Y
- Update documentation

Closes #123"
```

### 7. Push and Create PR

```bash
git push origin feature/your-feature-name
```

Then create a Pull Request on GitHub.

## Commit Message Convention

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation changes |
| `style` | Code style changes (formatting, etc.) |
| `refactor` | Code refactoring |
| `perf` | Performance improvements |
| `test` | Test additions/changes |
| `chore` | Maintenance tasks |
| `build` | Build system changes |
| `ci` | CI configuration changes |

### Examples

```
feat(crawler): add incremental crawling support

fix(rewrite): handle data: URLs in CSS

docs(api): update REST API documentation

perf(network): add interceptor pooling

test(queue): add Redis queue integration tests

chore(deps): update chromedp to v0.16.0
```

## Code Style

### Go Style Guide

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` / `goimports`
- Run `go vet` before committing
- Follow package naming conventions

### Naming Conventions

- Packages: lowercase, single word (`crawler`, `network`)
- Interfaces: noun + `er` (`Crawler`, `Storage`)
- Private: lowercase (`privateField`)
- Public: PascalCase (`PublicMethod`)
- Constants: PascalCase (`DefaultTimeout`)
- Errors: `Err` prefix (`ErrNotFound`)

### Error Handling

```go
// Good
if err != nil {
    return fmt.Errorf("failed to crawl %s: %w", url, err)
}

// Avoid
if err != nil {
    return err
}
```

### Logging

```go
// Use structured logging
util.LogInfo("starting crawl",
    zap.String("url", url),
    zap.Int("depth", depth),
)

// Errors
util.LogError("crawl failed", err,
    zap.String("url", url),
)
```

## Testing Guidelines

### Unit Tests

- Test public API
- Use table-driven tests
- Mock external dependencies
- Aim for >80% coverage

```go
func TestNormalizeURL(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"simple", "http://example.com", "http://example.com/"},
        {"with_path", "http://example.com/path", "http://example.com/path"},
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

### Integration Tests

- Tag with `//go:build integration`
- Use testcontainers for Chrome
- Clean up resources in `defer`

### Benchmarks

```go
func BenchmarkRewriteHTML(b *testing.B) {
    html := generateHTML(10000)
    rw := rewrite.NewRewriter()
    optRw := rewrite.NewOptimizedRewriter(rw)
    optRw.Initialize("/tmp", "https://example.com")
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = optRw.RewriteHTMLOptimized([]byte(html), "/tmp/page.html")
    }
}
```

## Pull Request Process

### PR Requirements

- [ ] All tests pass (`go test ./...`)
- [ ] Race detector clean (`go test -race ./...`)
- [ ] No vet warnings (`go vet ./...`)
- [ ] Code formatted (`gofmt`)
- [ ] Lint clean (`golangci-lint run`)
- [ ] No new vulnerabilities (`govulncheck ./...`)
- [ ] Tests added for new functionality
- [ ] Documentation updated
- [ ] CHANGELOG.md updated

### PR Template

```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Race detector clean
- [ ] Benchmarks run (if performance related)

## Checklist
- [ ] Code formatted
- [ ] Tests added
- [ ] Documentation updated
- [ ] CHANGELOG.md updated
- [ ] No breaking changes (or documented)
```

### Review Process

1. Automated checks must pass
2. At least one maintainer approval required
3. All conversations resolved
4. Squash and merge

## Release Process

### Versioning

We use [Semantic Versioning](https://semver.org/):

- `MAJOR`: Breaking changes
- `MINOR`: New features (backward compatible)
- `PATCH`: Bug fixes (backward compatible)

### Release Steps

1. Update version in `cmd/clone/main.go`
2. Update `CHANGELOG.md`
4. Tag release: `git tag -a v1.2.3 -m "Release v1.2.3"`
5. Push tag: `git push origin v1.2.3`
5. CI builds and publishes release

## Community

### Communication Channels

- GitHub Issues: Bug reports, feature requests
- GitHub Discussions: General questions, ideas
- Discord/Slack: Real-time chat (if available)

### Getting Help

- Check existing issues first
- Search documentation
- Ask in discussions
- Create minimal reproduction case

## Recognition

Contributors are recognized in:
- `CONTRIBUTORS.md` file
- Release notes
- GitHub contributors graph

Thank you for contributing to Web Cloner! 🎉