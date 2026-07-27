package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	limiters    map[string]*hostLimiter
	mu          sync.RWMutex
	defaultRate time.Duration
	minDelay    time.Duration
	maxDelay    time.Duration
	burstSize   int
	cleanupAt   time.Time
	cleanupDone chan struct{}
	stopOnce    sync.Once
}

type hostLimiter struct {
	limiter     *rate.Limiter
	lastSeen    time.Time
	lastLatency time.Duration
	mu          sync.Mutex
}

func New(ctx context.Context, defaultDelay time.Duration, burstSize int) *RateLimiter {
	if burstSize < 1 {
		burstSize = 1
	}
	rl := &RateLimiter{
		limiters:    make(map[string]*hostLimiter),
		defaultRate: defaultDelay,
		minDelay:    100 * time.Millisecond,
		maxDelay:    30 * time.Second,
		burstSize:   burstSize,
		cleanupAt:   time.Now().Add(5 * time.Minute),
		cleanupDone: make(chan struct{}),
	}
	go rl.periodicCleanup(ctx)
	return rl
}

func (rl *RateLimiter) get(host string, baseDelay time.Duration) *rate.Limiter {
	rl.mu.RLock()
	hl, ok := rl.limiters[host]
	rl.mu.RUnlock()

	if ok {
		hl.mu.Lock()
		hl.lastSeen = time.Now()
		delay := baseDelay
		if delay <= 0 {
			delay = rl.defaultRate
		}
		if hl.limiter.Limit() != rate.Every(delay) {
			hl.limiter.SetLimit(rate.Every(delay))
		}
		hl.mu.Unlock()
		return hl.limiter
	}

	delay := baseDelay
	if delay <= 0 {
		delay = rl.defaultRate
	}

	rl.mu.Lock()
	if hl, ok := rl.limiters[host]; ok {
		rl.mu.Unlock()
		return hl.limiter
	}

	hl = &hostLimiter{
		limiter:  rate.NewLimiter(rate.Every(delay), rl.burstSize),
		lastSeen: time.Now(),
	}
	rl.limiters[host] = hl
	rl.mu.Unlock()
	return hl.limiter
}

func (rl *RateLimiter) Wait(ctx context.Context, host string, baseDelay time.Duration) error {
	limiter := rl.get(host, baseDelay)
	return limiter.Wait(ctx)
}

func (rl *RateLimiter) Allow(host string, baseDelay time.Duration) bool {
	limiter := rl.get(host, baseDelay)
	return limiter.Allow()
}

func (rl *RateLimiter) ObserveLatency(host string, latency time.Duration) {
	rl.mu.RLock()
	hl, ok := rl.limiters[host]
	rl.mu.RUnlock()
	if !ok {
		return
	}

	hl.mu.Lock()
	defer hl.mu.Unlock()

	hl.lastSeen = time.Now()

	if hl.lastLatency == 0 {
		hl.lastLatency = latency
		return
	}

	hl.lastLatency = time.Duration(
		0.7*float64(hl.lastLatency) + 0.3*float64(latency),
	)

	currentRate := hl.limiter.Limit()
	delay := time.Duration(float64(time.Second) / float64(currentRate))

	if hl.lastLatency > delay*3 {
		newDelay := time.Duration(math.Min(float64(delay*2), float64(rl.maxDelay)))
		hl.limiter.SetLimit(rate.Every(newDelay))
	} else if hl.lastLatency < delay/3 && delay > rl.minDelay {
		newDelay := time.Duration(math.Max(float64(delay/2), float64(rl.minDelay)))
		hl.limiter.SetLimit(rate.Every(newDelay))
	}
}

func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for host, hl := range rl.limiters {
		if now.Sub(hl.lastSeen) > 10*time.Minute {
			delete(rl.limiters, host)
		}
	}
	rl.cleanupAt = now.Add(5 * time.Minute)
}

func (rl *RateLimiter) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.Cleanup()
		case <-rl.cleanupDone:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.cleanupDone)
	})
}

func (rl *RateLimiter) Len() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.limiters)
}
