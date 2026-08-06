package crawler

import (
	"context"
	"time"

	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/resilience"
	"github.com/user/clone/internal/storage"
)

// BrowserPool is the interface for managing browser instances.
type BrowserPool interface {
	Acquire() (context.Context, error)
	Close()
	Start() error
	HealthCheck()
}

// StorageBackend is the interface for storage operations.
type StorageBackend interface {
	SaveHTML(url string, content []byte) (string, error)
	SaveShadowDOM(url string, data []byte) error
	SaveStructuredData(url string, data []byte) (string, error)
	SaveArticle(url string, data []byte) (string, error)
	SaveSingleFile(url string, data []byte) (string, error)
	SaveAPI(url string, data []byte) error
	PathForAPI(url string) string
	WriteRecord(record *storage.WARCRecord) error
}

// QueueBackend is the interface for URL queue operations.
type QueueBackend interface {
	PushURL(url string, depth int) bool
	PopURL() (queue.URLItem, bool)
	Size() int
	HasSeen(url string) bool
	MarkSeen(url string)
	Items() []queue.URLItem
	AllVisited() map[string]bool
	Snapshot() ([]queue.URLItem, map[string]bool)
	LoadFromCheckpoint(items []queue.URLItem, visited map[string]bool)
	Close() error
}

// RateLimiterBackend is the interface for rate limiting.
type RateLimiterBackend interface {
	Allow(host string) bool
	Wait(host string)
	UpdateDelay(host string, delay time.Duration)
	Stop()
}

// CircuitBreakerBackend is the interface for circuit breaking.
type CircuitBreakerBackend interface {
	Allow(host string) bool
	Success(host string)
	Failure(host string)
	State(host string) resilience.State
	Reset(host string)
	Cleanup()
}

// RewriterBackend is the interface for URL rewriting.
type RewriterBackend interface {
	AddMapping(originalURL, localPath string)
	ExtractLinks(baseURL string, htmlContent []byte) []string
	RewriteHTML(htmlContent []byte, htmlLocalPath string) []byte
	RewriteCSS(cssContent []byte, cssLocalPath string) []byte
	ResolveURL(baseURL, relativeURL string) string
}

// NetworkInterceptorBackend is the interface for network interception.
type NetworkInterceptorBackend interface {
	Start(ctx context.Context, targetURL string)
	SetAPICallback(fn func(netintercept.APIResponse))
	FetchBodies(ctx context.Context)
	GetResources() map[string]*netintercept.CapturedResource
	GetAPIResponses() []netintercept.APIResponse
	GetMissingResources() []string
	DownloadResourceViaHTTP(rawURL string) (*netintercept.CapturedResource, error)
	Close()
}

// AuthManagerBackend is the interface for authentication.
type AuthManagerBackend interface {
	Authenticate(ctx context.Context, targetURL string) error
}
