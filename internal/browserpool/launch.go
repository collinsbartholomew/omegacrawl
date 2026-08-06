package browserpool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/systeminfo"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// contextErrorf is a chromedp error handler that drops benign unmarshal
// errors (newer Chrome sends enum values the pinned cdproto cannot decode,
// e.g. Network.IPAddressSpace "Loopback") and routes everything else through
// the structured logger.
func contextErrorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "could not unmarshal event") {
		return
	}
	util.LogError("chromedp", fmt.Errorf("%s", msg))
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

	// Create the persistent browser context once with the filtered error
	// handler; crawl tabs are derived from it and inherit the handler.
	browserCtx, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithErrorf(contextErrorf))

	// Allocate the browser on the persistent context. This must be the first
	// Run and must not use a timed child context: the browser is bound to the
	// context used for its first Run, so a timeout there would kill it. Use a
	// watchdog to bound startup instead.
	launchDone := make(chan error, 1)
	go func() { launchDone <- chromedp.Run(browserCtx, chromedp.Navigate("about:blank")) }()
	select {
	case err := <-launchDone:
		if err != nil {
			browserCancel()
			allocCancel()
			return nil, err
		}
	case <-time.After(30 * time.Second):
		browserCancel()
		allocCancel()
		return nil, errors.New("browser launch timed out")
	}

	inst := &BrowserInstance{
		allocCtx:      allocCtx,
		allocCancel:   allocCancel,
		browserCtx:    browserCtx,
		browserCancel: browserCancel,
		healthy:       true,
		lastUsed:      time.Now(),
	}

	if p.remoteURL == "" {
		discoCtx, discoCancel := context.WithTimeout(browserCtx, 10*time.Second)
		defer discoCancel()
		inst.proc, inst.pid = p.discoverBrowserProcess(discoCtx)
	}

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

		if i.browserCancel != nil {
			i.browserCancel()
		}
		if i.allocCancel != nil {
			i.allocCancel()
		}
	})
}
