package browserpool

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	// ErrPoolClosed is returned when the browser pool has been closed.
	ErrPoolClosed = errors.New("browser pool: closed")
	// ErrNoInstance is returned when no healthy browser instance is available.
	ErrNoInstance = errors.New("browser pool: no healthy instance available")
	// ErrAcquireFailed is returned when acquiring a browser instance fails.
	ErrAcquireFailed = errors.New("browser pool: acquire failed")
)

// BrowserInstance represents a single browser in the pool.
type BrowserInstance struct {
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	healthy       bool
	lastUsed      time.Time

	// proc is the Chrome browser's main OS process, tracked so the entire
	// process tree can be SIGKILLed and reaped on shutdown (preventing
	// zombie/orphaned child processes). Nil for remote allocators.
	proc *os.Process
	pid  int

	// shutdownOnce makes shutdown() idempotent so a browser being replaced by
	// the health check is never torn down a second time (and never spawns a
	// duplicate Wait goroutine).
	shutdownOnce sync.Once
}

// Pool manages a fixed set of browser instances for concurrent crawling.
type Pool struct {
	mu        sync.Mutex
	instances []*BrowserInstance
	size      int
	opts      []chromedp.ExecAllocatorOption
	ctx       context.Context
	closed    bool

	remoteURL string
}
