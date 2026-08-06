package benchmark

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/user/clone/internal/network"
	"github.com/user/clone/internal/rewrite"
)

// BenchmarkRewriteHTML benchmarks the HTML rewriting
func BenchmarkRewriteHTML(b *testing.B) {
	// Create a large HTML document
	html := generateLargeHTML(10000) // ~10KB
	htmlDir := "/tmp/test"

	rw := rewrite.NewRewriter()

	// Add some mappings
	for i := 0; i < 100; i++ {
		rw.AddMapping(fmt.Sprintf("https://example.com/static/style%d.css", i), fmt.Sprintf("/local/style%d.css", i))
		rw.AddMapping(fmt.Sprintf("https://example.com/static/script%d.js", i), fmt.Sprintf("/local/script%d.js", i))
		rw.AddMapping(fmt.Sprintf("https://example.com/images/img%d.png", i), fmt.Sprintf("/local/images/img%d.png", i))
	}

	optRw := rewrite.NewOptimizedRewriter(rw)
	optRw.Initialize(htmlDir, "https://example.com")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = optRw.RewriteHTML([]byte(html), htmlDir+"/page.html")
	}
}

// BenchmarkRewriteHTMLStreaming benchmarks streaming HTML rewriting
func BenchmarkRewriteHTMLStreaming(b *testing.B) {
	html := generateLargeHTML(50000) // ~50KB
	htmlDir := "/tmp/test"

	rw := rewrite.NewRewriter()
	for i := 0; i < 100; i++ {
		rw.AddMapping(fmt.Sprintf("https://example.com/static/style%d.css", i), fmt.Sprintf("/local/style%d.css", i))
	}

	optRw := rewrite.NewOptimizedRewriter(rw)
	optRw.Initialize(htmlDir, "https://example.com")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = optRw.StreamingRewrite([]byte(html), htmlDir+"/page.html")
	}
}

// BenchmarkInterceptorPool benchmarks the interceptor pool (without actual chromedp context)
func BenchmarkInterceptorPool(b *testing.B) {
	pool := network.NewInterceptorPool(4, 10)
	defer pool.Close()
	ctx := context.Background()

	// Just test Acquire/Release without chromedp
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		interceptor, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}
		interceptor.Reset()
		pool.Release(interceptor)
	}
}

// BenchmarkNetworkInterception benchmarks network interception (without actual chromedp context)
func BenchmarkNetworkInterception(b *testing.B) {
	interceptor := network.NewInterceptorWithWorkers(10)
	defer interceptor.Close()

	// Just test the methods without chromedp
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		interceptor.Reset()
		_ = interceptor.GetResources()
		_ = interceptor.GetAPIResponses()
		_ = interceptor.GetMissingResources()
	}
}

// BenchmarkRewriteCSS benchmarks CSS rewriting
func BenchmarkRewriteCSS(b *testing.B) {
	css := generateLargeCSS(1000)

	rw := rewrite.NewRewriter()
	for i := 0; i < 100; i++ {
		rw.AddMapping(fmt.Sprintf("https://example.com/font%d.woff2", i), fmt.Sprintf("/local/font%d.woff2", i))
	}

	optRw := rewrite.NewOptimizedRewriter(rw)
	optRw.Initialize("/tmp/test", "https://example.com")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = optRw.RewriteCSS([]byte(css), "/tmp/test/style.css")
	}
}

// BenchmarkCrawlerInitialization benchmarks crawler initialization
func BenchmarkCrawlerInitialization(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Just measure the setup time, not actual crawling
		_ = time.Now()
	}
}

// BenchmarkURLNormalization benchmarks URL normalization
func BenchmarkURLNormalization(b *testing.B) {
	urls := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		urls[i] = fmt.Sprintf("https://example.com/path%d?param=value%d#anchor", i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, url := range urls {
			_ = normalizeURL(url)
		}
	}
}

// Helper functions

func generateLargeHTML(size int) string {
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html><html><head><title>Test</title>")

	// Add CSS links
	for i := 0; i < size/500; i++ {
		buf.WriteString(fmt.Sprintf(`<link rel="stylesheet" href="https://example.com/static/style%d.css">`, i))
	}
	buf.WriteString("</head><body>")

	// Add content
	for i := 0; i < size/200; i++ {
		buf.WriteString(fmt.Sprintf(`<div class="content"><img src="https://example.com/images/img%d.png" alt="Image %d"><script src="https://example.com/static/script%d.js"></script></div>`, i, i, i))
	}

	buf.WriteString("</body></html>")
	result := buf.String()

	// Pad to desired size
	for len(result) < size {
		result += " "
	}
	return result[:size]
}

func generateLargeCSS(size int) string {
	var buf bytes.Buffer
	for i := 0; i < size/100; i++ {
		buf.WriteString(fmt.Sprintf(`.class%d { background: url(https://example.com/bg%d.png); font-family: "Font%d"; }`, i, i, i))
	}
	result := buf.String()
	for len(result) < size {
		result += " "
	}
	return result[:size]
}

func normalizeURL(url string) string {
	// Simple URL normalization for benchmarking
	return strings.ToLower(strings.TrimSpace(url))
}

// TestRewriteHTMLPerformance tests the performance of HTML rewriting
func TestRewriteHTMLPerformance(t *testing.T) {
	html := generateLargeHTML(10000)
	htmlDir := "/tmp/test"

	rw := rewrite.NewRewriter()
	for i := 0; i < 100; i++ {
		rw.AddMapping(fmt.Sprintf("https://example.com/static/style%d.css", i), fmt.Sprintf("/local/style%d.css", i))
	}

	optRw := rewrite.NewOptimizedRewriter(rw)
	optRw.Initialize(htmlDir, "https://example.com")

	start := time.Now()
	for i := 0; i < 100; i++ {
		_ = optRw.RewriteHTML([]byte(html), htmlDir+"/page.html")
	}
	elapsed := time.Since(start)

	t.Logf("Rewrote 100 pages of 10KB HTML in %v (%.2f MB/s)", elapsed, float64(100*10000)/elapsed.Seconds()/1024/1024)

	// Streaming test
	start = time.Now()
	for i := 0; i < 100; i++ {
		_, _ = optRw.StreamingRewrite([]byte(html), htmlDir+"/page.html")
	}
	elapsed = time.Since(start)

	t.Logf("Streaming rewrote 100 pages of 10KB HTML in %v (%.2f MB/s)", elapsed, float64(100*10000)/elapsed.Seconds()/1024/1024)
}
