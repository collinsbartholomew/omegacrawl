package crawler

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type HostRateLimiter struct {
	limiters    sync.Map
	defaultRate time.Duration
	burstSize   int
}

func NewHostRateLimiter(defaultRate time.Duration, burstSize int) *HostRateLimiter {
	return &HostRateLimiter{
		defaultRate: defaultRate,
		burstSize:   burstSize,
	}
}

func (h *HostRateLimiter) GetLimiter(host string, crawlDelay time.Duration) *rate.Limiter {
	limiterIface, ok := h.limiters.Load(host)
	if ok {
		return limiterIface.(*rate.Limiter)
	}

	rateLimit := rate.Every(h.defaultRate)
	if crawlDelay > 0 {
		rateLimit = rate.Every(crawlDelay)
	}

	limiter := rate.NewLimiter(rateLimit, h.burstSize)
	actual, loaded := h.limiters.LoadOrStore(host, limiter)
	if loaded {
		return actual.(*rate.Limiter)
	}
	return limiter
}

func (h *HostRateLimiter) Wait(ctx context.Context, host string, crawlDelay time.Duration) {
	limiter := h.GetLimiter(host, crawlDelay)
	limiter.Wait(ctx)
}

func (h *HostRateLimiter) Allow(host string, crawlDelay time.Duration) bool {
	limiter := h.GetLimiter(host, crawlDelay)
	return limiter.Allow()
}

func (h *HostRateLimiter) Cleanup() {
	h.limiters.Range(func(key, value interface{}) bool {
		h.limiters.Delete(key)
		return true
	})
}
