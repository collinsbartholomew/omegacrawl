//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ChromeContainer wraps a testcontainers Chrome container
type ChromeContainer struct {
	Container testcontainers.Container
	Endpoint  string
}

// StartChrome starts a Chrome container for integration testing
func StartChrome(ctx context.Context) (*ChromeContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "zenika/alpine-chrome:latest",
		ExposedPorts: []string{"9222/tcp"},
		WaitingFor:   wait.ForListeningPort("9222/tcp").WithStartupTimeout(60 * time.Second),
		Cmd: []string{
			"--no-sandbox",
			"--disable-gpu",
			"--disable-dev-shm-usage",
			"--remote-debugging-address=0.0.0.0",
			"--remote-debugging-port=9222",
			"--disable-setuid-sandbox",
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start chrome container: %w", err)
	}

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get endpoint: %w", err)
	}

	wsEndpoint := fmt.Sprintf("ws://%s/devtools/browser", endpoint)

	return &ChromeContainer{
		Container: container,
		Endpoint:  wsEndpoint,
	}, nil
}

// Stop stops the Chrome container
func (c *ChromeContainer) Stop(ctx context.Context) error {
	return c.Container.Terminate(ctx)
}

// NewTestCrawler creates a crawler configured for integration testing
func NewTestCrawler(ctx context.Context, chromeEndpoint string) (*TestCrawler, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, chromeEndpoint)
	
	return &TestCrawler{
		ctx:    allocCtx,
		cancel: cancel,
	}, nil
}

// TestCrawler provides a simple interface for integration tests
type TestCrawler struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Navigate navigates to a URL and returns the page content
func (tc *TestCrawler) Navigate(url string) (string, error) {
	var html string
	err := chromedp.Run(tc.ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	)
	return html, err
}

// Close cleans up the test crawler
func (tc *TestCrawler) Close() {
	tc.cancel()
}

// WaitForHealthy waits for the Chrome container to be ready
func WaitForHealthy(ctx context.Context, endpoint string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	
	// Extract host:port from ws:// endpoint
	// ws://host:port/devtools/browser -> host:port
	var hostPort string
	fmt.Sscanf(endpoint, "ws://%s/devtools/browser", &hostPort)
	
	healthURL := fmt.Sprintf("http://%s/json/version", hostPort)
	
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("chrome not healthy after %v", timeout)
}