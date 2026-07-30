package browserpool

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

var (
	ErrPoolClosed    = errors.New("browser pool: closed")
	ErrNoInstance    = errors.New("browser pool: no healthy instance available")
	ErrAcquireFailed = errors.New("browser pool: acquire failed")
)

type BrowserInstance struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	browserCtx  context.Context
	healthy     bool
	lastUsed    time.Time
}

type Pool struct {
	mu       sync.Mutex
	instances []*BrowserInstance
	size     int
	opts     []chromedp.ExecAllocatorOption
	ctx      context.Context
	closed   bool

	remoteURL string
}

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

func (p *Pool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.instances = make([]*BrowserInstance, 0, p.size)
	for i := 0; i < p.size; i++ {
		inst, err := p.launchInstance()
		if err != nil {
			// Cleanup already-launched instances
			for _, inst := range p.instances {
				inst.allocCancel()
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
		allocCtx, allocCancel = chromedp.NewExecAllocator(p.ctx, p.opts...)
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
	verifyCancel()

	return &BrowserInstance{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		browserCtx:  allocCtx,
		healthy:     true,
		lastUsed:    time.Now(),
	}, nil
}

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
			inst.allocCancel()

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
			inst.allocCancel()

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

func (p *Pool) HealthCheck() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, inst := range p.instances {
		if inst.browserCtx.Err() != nil {
			inst.healthy = false
			util.LogInfo("browser instance unhealthy", zap.Int("index", i), zap.Error(inst.browserCtx.Err()))

			newInst, err := p.launchInstance()
			if err != nil {
				util.LogError("failed to replace unhealthy browser", err, zap.Int("index", i))
				continue
			}
			inst.allocCancel()
			p.instances[i] = newInst
			util.LogInfo("browser instance replaced", zap.Int("index", i))
		}
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true

	for i, inst := range p.instances {
		util.LogInfo("shutting down browser instance", zap.Int("index", i))
		inst.allocCancel()
		// Wait briefly for Chrome to exit
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		select {
		case <-inst.allocCtx.Done():
		case <-waitCtx.Done():
		}
		waitCancel()
	}
	p.instances = nil
	util.LogInfo("browser pool shut down")
}
