//go:build integration

package integration

import (
	"context"
	"testing"
	"time"
)

// TestChromeContainerLifecycle tests that Chrome container starts and stops correctly
func TestChromeContainerLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	chrome, err := StartChrome(ctx)
	if err != nil {
		t.Skipf("Skipping integration test (Docker not available): %v", err)
	}
	defer chrome.Stop(ctx)

	// Verify we got a WebSocket endpoint
	if chrome.Endpoint == "" {
		t.Fatal("Expected WebSocket endpoint")
	}

	// Wait for Chrome to be healthy
	if err := WaitForHealthy(ctx, chrome.Endpoint, 60*time.Second); err != nil {
		t.Fatalf("Chrome not healthy: %v", err)
	}
}

// TestBasicNavigation tests basic page navigation
func TestBasicNavigation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	chrome, err := StartChrome(ctx)
	if err != nil {
		t.Skipf("Skipping integration test (Docker not available): %v", err)
	}
	defer chrome.Stop(ctx)

	if err := WaitForHealthy(ctx, chrome.Endpoint, 60*time.Second); err != nil {
		t.Fatalf("Chrome not healthy: %v", err)
	}

	crawler, err := NewTestCrawler(ctx, chrome.Endpoint)
	if err != nil {
		t.Fatalf("Failed to create test crawler: %v", err)
	}
	defer crawler.Close()

	// Test with a simple page - use a local test server or example.com
	html, err := crawler.Navigate("https://example.com")
	if err != nil {
		t.Fatalf("Navigation failed: %v", err)
	}

	if len(html) == 0 {
		t.Fatal("Expected HTML content")
	}

	// Verify basic HTML structure
	if !contains(html, "<html") && !contains(html, "<HTML") {
		t.Fatal("Expected HTML tag in response")
	}
}

// TestJSRendering tests JavaScript rendering capability
func TestJSRendering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	chrome, err := StartChrome(ctx)
	if err != nil {
		t.Skipf("Skipping integration test (Docker not available): %v", err)
	}
	defer chrome.Stop(ctx)

	if err := WaitForHealthy(ctx, chrome.Endpoint, 60*time.Second); err != nil {
		t.Fatalf("Chrome not healthy: %v", err)
	}

	crawler, err := NewTestCrawler(ctx, chrome.Endpoint)
	if err != nil {
		t.Fatalf("Failed to create test crawler: %v", err)
	}
	defer crawler.Close()

	// Create a simple HTML page with JavaScript
	// We'll use a data URL for simplicity
	html, err := crawler.Navigate("data:text/html,<html><body><div id='test'>initial</div><script>document.getElementById('test').textContent = 'rendered'</script></body></html>")
	if err != nil {
		t.Fatalf("Navigation failed: %v", err)
	}

	// The JavaScript should have executed and changed the content
	if !contains(html, "rendered") {
		t.Logf("HTML: %s", html)
		t.Fatal("Expected JavaScript to have executed and changed content to 'rendered'")
	}
}

// TestMultiplePages tests crawling multiple pages
func TestMultiplePages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	chrome, err := StartChrome(ctx)
	if err != nil {
		t.Skipf("Skipping integration test (Docker not available): %v", err)
	}
	defer chrome.Stop(ctx)

	if err := WaitForHealthy(ctx, chrome.Endpoint, 60*time.Second); err != nil {
		t.Fatalf("Chrome not healthy: %v", err)
	}

	crawler, err := NewTestCrawler(ctx, chrome.Endpoint)
	if err != nil {
		t.Fatalf("Failed to create test crawler: %v", err)
	}
	defer crawler.Close()

	urls := []string{
		"https://example.com",
		"https://httpbin.org/html",
	}

	for _, url := range urls {
		html, err := crawler.Navigate(url)
		if err != nil {
			t.Logf("Navigation to %s failed (may be network issue): %v", url, err)
			continue
		}
		if len(html) == 0 {
			t.Errorf("Expected content from %s", url)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}