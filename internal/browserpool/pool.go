package browserpool

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/systeminfo"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
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
	allocCtx    context.Context
	allocCancel context.CancelFunc
	browserCtx  context.Context
	healthy     bool
	lastUsed    time.Time

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

// New creates a browser pool with the given size and allocator options.
func New(ctx context.Context, size int, opts []chromedp.ExecAllocatorOption, remoteURL string) *Pool {
	if size < 1 {
		size = 1
	}
	p := &Pool{
		size:      size,
		opts:      opts,
		ctx:       ctx,
		remoteURL: remoteURL,
	}
	return p
}

// Start launches all browser instances in the pool.
func (p *Pool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.instances = make([]*BrowserInstance, 0, p.size)
	for i := 0; i < p.size; i++ {
		inst, err := p.launchInstance()
		if err != nil {
			// Cleanup already-launched instances
			for _, inst := range p.instances {
				inst.shutdown()
			}
			p.instances = nil
			return err
		}
		p.instances = append(p.instances, inst)
	}
	util.LogInfo("browser pool started", zap.Int("size", p.size))
	return nil
}

func (p *Pool) launchInstance() (*BrowserInstance, error) {
	var allocCtx context.Context
	var allocCancel context.CancelFunc

	if p.remoteURL != "" {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(p.ctx, p.remoteURL)
	} else {
		opts := append([]chromedp.ExecAllocatorOption{}, p.opts...)
		opts = append(opts, chromedp.ModifyCmdFunc(modifyChromeCmd))
		allocCtx, allocCancel = chromedp.NewExecAllocator(p.ctx, opts...)
	}

	// Create a tab to verify the browser launched (with timeout)
	verifyCtx, verifyCancel := chromedp.NewContext(allocCtx)
	verifyCtx, verifyTimeoutCancel := context.WithTimeout(verifyCtx, 30*time.Second)
	defer verifyTimeoutCancel()

	if err := chromedp.Run(verifyCtx, chromedp.Navigate("about:blank")); err != nil {
		verifyCancel()
		allocCancel()
		return nil, err
	}

	inst := &BrowserInstance{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		browserCtx:  allocCtx,
		healthy:     true,
		lastUsed:    time.Now(),
	}

	// Discover the browser process so we can SIGKILL + reap it (and its
	// children) on shutdown instead of leaving zombies behind.
	if p.remoteURL == "" {
		inst.proc, inst.pid = p.discoverBrowserProcess(verifyCtx)
	}
	verifyCancel()

	return inst, nil
}

// discoverBrowserProcess asks Chrome (via CDP) for its main process PID and
// wraps it in an *os.Process handle for later reaping.
func (p *Pool) discoverBrowserProcess(verifyCtx context.Context) (*os.Process, int) {
	pid := int64(0)
	err := chromedp.Run(verifyCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Browser == nil {
			return errors.New("browser not available")
		}
		procs, err := systeminfo.GetProcessInfo().Do(cdp.WithExecutor(ctx, c.Browser))
		if err != nil {
			return err
		}
		for _, proc := range procs {
			if proc.Type == "browser" {
				pid = proc.ID
				return nil
			}
		}
		return errors.New("browser process not found")
	}))
	if err != nil || pid <= 0 {
		util.LogError("failed to discover browser process", err)
		return nil, 0
	}
	proc, err := os.FindProcess(int(pid))
	if err != nil {
		return nil, 0
	}
	return proc, int(pid)
}

// shutdown tears down the Chrome process tree: SIGKILLs the process group
// (killing all renderer/GPU children), waits briefly for the main process to
// be reaped, then cancels the chromedp allocator. It is idempotent.
func (i *BrowserInstance) shutdown() {
	i.shutdownOnce.Do(func() {
		if i.proc != nil && i.pid > 0 {
			killProcessTree(i.pid)

			done := make(chan struct{})
			go func() {
				i.proc.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}

		if i.allocCancel != nil {
			i.allocCancel()
		}
	})
}

// Acquire returns a context for an available browser instance, or an error if none is healthy.
func (p *Pool) Acquire() (context.Context, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, nil, ErrPoolClosed
	}

	if len(p.instances) == 0 {
		return nil, nil, ErrNoInstance
	}

	// Try to find a healthy instance, starting with least-recently-used
	inst := p.selectInstance()
	if inst == nil {
		// All unhealthy — try restarting one
		inst = p.tryRestartInstance()
	}
	if inst == nil {
		return nil, nil, ErrNoInstance
	}

	// Check if browser is still alive
	if inst.browserCtx.Err() != nil {
		inst.healthy = false
		inst = p.tryRestartInstance()
		if inst == nil {
			return nil, nil, ErrNoInstance
		}
	}

	inst.lastUsed = time.Now()

	return inst.browserCtx, func() {}, nil
}

func (p *Pool) selectInstance() *BrowserInstance {
	// Pick the least-recently-used healthy instance
	var best *BrowserInstance
	for _, inst := range p.instances {
		if inst.healthy && (best == nil || inst.lastUsed.Before(best.lastUsed)) {
			best = inst
		}
	}
	return best
}

func (p *Pool) tryRestartInstance() *BrowserInstance {
	for i, inst := range p.instances {
		if !inst.healthy {
			util.LogInfo("restarting unhealthy browser instance", zap.Int("index", i))
			inst.shutdown()

			newInst, err := p.launchInstance()
			if err != nil {
				util.LogError("failed to restart browser instance", err, zap.Int("index", i))
				continue
			}
			p.instances[i] = newInst
			return newInst
		}
	}

	// All are marked healthy but could be actually dead
	for i, inst := range p.instances {
		if inst.browserCtx.Err() != nil {
			util.LogInfo("restarting dead browser instance", zap.Int("index", i))
			inst.shutdown()

			newInst, err := p.launchInstance()
			if err != nil {
				util.LogError("failed to restart browser instance", err, zap.Int("index", i))
				continue
			}
			p.instances[i] = newInst
			return newInst
		}
	}

	return nil
}

// HealthCheck replaces any unhealthy browser instances in the pool.
func (p *Pool) HealthCheck() {
	p.mu.Lock()
	// Collect unhealthy instances to replace.
	var toReplace []int
	for i, inst := range p.instances {
		if inst.browserCtx.Err() != nil {
			inst.healthy = false
			util.LogInfo("browser instance unhealthy", zap.Int("index", i), zap.Error(inst.browserCtx.Err()))
			toReplace = append(toReplace, i)
		}
	}
	if len(toReplace) == 0 {
		p.mu.Unlock()
		return
	}

	// Snapshot the instances slice so replacements can be written back while
	// the lock is released during the blocking launch/shutdown calls.
	instances := p.instances
	p.mu.Unlock()

	for _, i := range toReplace {
		old := instances[i]
		// Shut the old browser down first so its profile lock and ports are
		// released before the replacement launches (avoids launch failure).
		old.shutdown()

		newInst, err := p.launchInstance()
		if err != nil {
			util.LogError("failed to replace unhealthy browser", err, zap.Int("index", i))
			continue
		}

		p.mu.Lock()
		if p.closed || i >= len(p.instances) {
			p.mu.Unlock()
			newInst.shutdown()
			continue
		}
		p.instances[i] = newInst
		p.mu.Unlock()
		util.LogInfo("browser instance replaced", zap.Int("index", i))
	}
}

// Close shuts down all browser instances and marks the pool closed.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	instances := p.instances
	p.instances = nil
	p.mu.Unlock()

	// Shut instances down outside the lock: each shutdown may block up to 5s
	// waiting on the Chrome process, and holding the mutex would stall every
	// concurrent Acquire caller.
	for _, inst := range instances {
		util.LogInfo("shutting down browser instance")
		inst.shutdown()
	}
	util.LogInfo("browser pool shut down")
}
