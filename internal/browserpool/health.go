package browserpool

import (
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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

	instances := p.instances
	p.mu.Unlock()

	for _, i := range toReplace {
		old := instances[i]

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

	for _, inst := range instances {
		util.LogInfo("shutting down browser instance")
		inst.shutdown()
	}
	util.LogInfo("browser pool shut down")
}
