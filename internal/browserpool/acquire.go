package browserpool

import (
	"context"
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// Acquire returns a context for an available browser instance, or an error if none is healthy.
func (p *Pool) Acquire() (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrPoolClosed
	}

	if len(p.instances) == 0 {
		return nil, ErrNoInstance
	}

	inst := p.selectInstance()
	if inst == nil {

		inst = p.tryRestartInstance()
	}
	if inst == nil {
		return nil, ErrNoInstance
	}

	if inst.browserCtx.Err() != nil {
		inst.healthy = false
		inst = p.tryRestartInstance()
		if inst == nil {
			return nil, ErrNoInstance
		}
	}

	inst.lastUsed = time.Now()

	return inst.browserCtx, nil
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
