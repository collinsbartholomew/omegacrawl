package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/user/clone/internal/network"
	"github.com/user/clone/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// AssetDownloadRequest represents a single asset to download
type AssetDownloadRequest struct {
	URL        string
	Referer    string
	HTMLDir    string
	MimeType   string
	Headers    map[string]string
	Priority   int // Higher = more important
}

// AssetDownloadResult represents the result of an asset download
type AssetDownloadResult struct {
	Request   AssetDownloadRequest
	LocalPath string
	Body      []byte
	Error     error
	Size      int64
	Duration  time.Duration
	StatusCode int
}

// BatchAssetDownloader downloads multiple assets in batches using connection pooling
type BatchAssetDownloader struct {
	client      *http.Client
	workerCount int
	queue       chan AssetDownloadRequest
	results     chan AssetDownloadResult
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	stats       BatchDownloadStats
}

// BatchDownloadStats tracks download statistics
type BatchDownloadStats struct {
	mu              sync.Mutex
	TotalRequests   int64
	Completed       int64
	Failed          int64
	TotalBytes      int64
	TotalDuration   time.Duration
	AvgDuration     time.Duration
}

// NewBatchAssetDownloader creates a new batch asset downloader
func NewBatchAssetDownloader(workerCount int, client *http.Client) *BatchAssetDownloader {
	ctx, cancel := context.WithCancel(context.Background())
	
	if workerCount <= 0 {
		workerCount = 16 // Default to asset concurrency
	}
	
	return &BatchAssetDownloader{
		client:      client,
		workerCount: workerCount,
		queue:       make(chan AssetDownloadRequest, workerCount*4),
		results:     make(chan AssetDownloadResult, workerCount*4),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start starts the worker pool
func (b *BatchAssetDownloader) Start() {
	for i := 0; i < b.workerCount; i++ {
		b.wg.Add(1)
		go b.worker(i)
	}
}

// Stop stops the downloader
func (b *BatchAssetDownloader) Stop() {
	b.cancel()
	b.wg.Wait()
	close(b.queue)
	close(b.results)
}

// Submit adds a download request to the queue
func (b *BatchAssetDownloader) Submit(req AssetDownloadRequest) {
	select {
	case b.queue <- req:
		b.stats.mu.Lock()
		b.stats.TotalRequests++
		b.stats.mu.Unlock()
	case <-b.ctx.Done():
		// Context cancelled
	}
}

// Results returns the results channel
func (b *BatchAssetDownloader) Results() <-chan AssetDownloadResult {
	return b.results
}

// worker processes download requests
func (b *BatchAssetDownloader) worker(id int) {
	defer b.wg.Done()
	
	for {
		select {
		case req, ok := <-b.queue:
			if !ok {
				return
			}
			
			result := b.downloadAsset(req)
			
			b.stats.mu.Lock()
			b.stats.Completed++
			if result.Error != nil {
				b.stats.Failed++
			} else {
				b.stats.TotalBytes += result.Size
				b.stats.TotalDuration += result.Duration
				if b.stats.Completed > 0 {
					b.stats.AvgDuration = b.stats.TotalDuration / time.Duration(b.stats.Completed)
				}
			}
			b.stats.mu.Unlock()
			
			// Send result (non-blocking with select)
			select {
			case b.results <- result:
			case <-b.ctx.Done():
				return
			}
			
		case <-b.ctx.Done():
			return
		}
	}
}

// downloadAsset downloads a single asset
func (b *BatchAssetDownloader) downloadAsset(req AssetDownloadRequest) AssetDownloadResult {
	ctx, span := tracing.StartSpan(b.ctx, "asset.download",
		tracing.WithAttribute("url", req.URL),
	)
	defer span.End()
	
	start := time.Now()
	
	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.URL, nil)
	if err != nil {
		return AssetDownloadResult{
			Request: req,
			Error:   fmt.Errorf("failed to create request: %w", err),
		}
	}
	
	// Set headers
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	httpReq.Header.Set("Accept", "*/*")
	if req.Referer != "" {
		httpReq.Header.Set("Referer", req.Referer)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	
	// Execute request
	resp, err := b.client.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		return AssetDownloadResult{
			Request:   req,
			Error:     fmt.Errorf("request failed: %w", err),
			Duration:  time.Since(start),
		}
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return AssetDownloadResult{
			Request:    req,
			Error:      fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status),
			StatusCode: resp.StatusCode,
			Duration:   time.Since(start),
		}
	}
	
	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return AssetDownloadResult{
			Request:    req,
			Error:      fmt.Errorf("failed to read body: %w", err),
			StatusCode: resp.StatusCode,
			Duration:   time.Since(start),
		}
	}
	
	span.SetAttributes(
		attribute.Int("size", len(body)),
		attribute.Int("status_code", resp.StatusCode),
	)
	
	return AssetDownloadResult{
		Request:    req,
		Body:       body,
		Size:       int64(len(body)),
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}
}

// GetStats returns current statistics
func (b *BatchAssetDownloader) GetStats() BatchDownloadStats {
	b.stats.mu.Lock()
	defer b.stats.mu.Unlock()
	return b.stats
}

// ParallelAssetDownloader provides high-level parallel asset downloading with batching
type ParallelAssetDownloader struct {
	downloader  *BatchAssetDownloader
	pendingReqs []AssetDownloadRequest
	mu          sync.Mutex
	results     map[string]AssetDownloadResult
}

// NewParallelAssetDownloader creates a new parallel asset downloader
func NewParallelAssetDownloader(workerCount int, client *http.Client) *ParallelAssetDownloader {
	return &ParallelAssetDownloader{
		downloader: NewBatchAssetDownloader(workerCount, client),
		results:    make(map[string]AssetDownloadResult),
	}
}

// AddRequest adds a request to the batch
func (p *ParallelAssetDownloader) AddRequest(req AssetDownloadRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingReqs = append(p.pendingReqs, req)
}

// Execute runs all pending requests in parallel and returns results
func (p *ParallelAssetDownloader) Execute(ctx context.Context) (map[string]AssetDownloadResult, error) {
	p.mu.Lock()
	requests := p.pendingReqs
	p.pendingReqs = nil
	p.mu.Unlock()
	
	if len(requests) == 0 {
		return p.results, nil
	}
	
	ctx, span := tracing.StartSpan(ctx, "asset.download.batch",
		tracing.WithAttribute("count", len(requests)),
	)
	defer span.End()
	
	// Start downloader
	p.downloader.Start()
	defer p.downloader.Stop()
	
	// Submit all requests
	for _, req := range requests {
		select {
		case p.downloader.queue <- req:
		case <-ctx.Done():
			return p.results, ctx.Err()
		}
	}
	
	// Collect results
	completed := 0
	for completed < len(requests) {
		select {
		case result := <-p.downloader.Results():
			// Find the original request and create result
			// Note: In a real implementation, we'd match by URL
			p.results[result.Request.URL] = result
			completed++
		case <-ctx.Done():
			return p.results, ctx.Err()
		}
	}
	
	stats := p.downloader.GetStats()
	span.SetAttributes(
		attribute.Int64("completed", stats.Completed),
		attribute.Int64("failed", stats.Failed),
		attribute.Int64("total_bytes", stats.TotalBytes),
	)
	
	return p.results, nil
}

// GetResults returns the download results
func (p *ParallelAssetDownloader) GetResults() map[string]AssetDownloadResult {
	return p.results
}

// AssetDownloadManager manages asset downloads across multiple pages
type AssetDownloadManager struct {
	client      *http.Client
	downloader  *ParallelAssetDownloader
	assetSem    chan struct{} // Semaphore for limiting concurrent asset downloads
	logger      *zap.Logger
}

// NewAssetDownloadManager creates a new asset download manager
func NewAssetDownloadManager(assetConcurrency int, client *http.Client) *AssetDownloadManager {
	if assetConcurrency <= 0 {
		assetConcurrency = 16
	}
	
	return &AssetDownloadManager{
		client:      client,
		downloader:  NewParallelAssetDownloader(assetConcurrency, client),
		assetSem:    make(chan struct{}, assetConcurrency),
		logger:      zap.L().Named("asset-downloader"),
	}
}

// DownloadAssets downloads all assets for a page in parallel
func (m *AssetDownloadManager) DownloadAssets(ctx context.Context, pageURL, htmlDir string, resources map[string]*network.CapturedResource) error {
	ctx, span := tracing.StartSpan(ctx, "asset.manager.download_page",
		tracing.WithAttribute("url", pageURL),
		tracing.WithAttribute("resource_count", len(resources)),
	)
	defer span.End()
	
	if len(resources) == 0 {
		return nil
	}
	
	// Build download requests
	var requests []AssetDownloadRequest
	for url, resource := range resources {
		// Skip if already saved (incremental)
		// Check MIME type for priority
		priority := 0
		if isCriticalResource(resource.MimeType) {
			priority = 10
		}
		
		req := AssetDownloadRequest{
			URL:      url,
			Referer:  pageURL,
			HTMLDir:  htmlDir,
			MimeType: resource.MimeType,
			Headers:  resource.Headers,
			Priority: priority,
		}
		requests = append(requests, req)
	}
	
	// Sort by priority (highest first)
	for i := 0; i < len(requests); i++ {
		for j := i + 1; j < len(requests); j++ {
			if requests[i].Priority < requests[j].Priority {
				requests[i], requests[j] = requests[j], requests[i]
			}
		}
	}
	
	// Add all requests
	for _, req := range requests {
		m.downloader.AddRequest(req)
	}
	
	// Execute batch download
	_, err := m.downloader.Execute(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}
	
	span.SetAttributes(
		attribute.Int("downloaded", len(requests)),
	)
	
	return nil
}

// isCriticalResource determines if a resource is critical for page rendering
func isCriticalResource(mimeType string) bool {
	criticalTypes := []string{
		"text/css",
		"application/javascript",
		"text/javascript",
		"application/x-javascript",
		"font/",
		"image/",
	}
	
	for _, t := range criticalTypes {
		if len(mimeType) >= len(t) && mimeType[:len(t)] == t {
			return true
		}
	}
	return false
}

// DownloadStats returns download statistics
func (m *AssetDownloadManager) DownloadStats() BatchDownloadStats {
	return m.downloader.downloader.GetStats()
}