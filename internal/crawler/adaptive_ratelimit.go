package crawler

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type AdaptiveRateLimiter struct {
	limiters    sync.Map
	defaultRate time.Duration
	minDelay    time.Duration
	maxDelay    time.Duration
	burstSize   int
}

type adaptiveLimiter struct {
	limiter     *rate.Limiter
	lastLatency time.Duration
	mu          sync.Mutex
}

func NewAdaptiveRateLimiter(defaultDelay time.Duration, burstSize int) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		defaultRate: defaultDelay,
		minDelay:    100 * time.Millisecond,
		maxDelay:    30 * time.Second,
		burstSize:   burstSize,
	}
}

func (h *AdaptiveRateLimiter) GetLimiter(host string, baseDelay time.Duration) *rate.Limiter {
	limiterIface, ok := h.limiters.Load(host)
	if ok {
		limiter := limiterIface.(*adaptiveLimiter).limiter
		// Ensure the rate matches the current baseDelay (e.g., from robots.txt)
		expectedLimit := rate.Every(baseDelay)
		if limiter.Limit() != expectedLimit {
			limiter.SetLimit(expectedLimit)
		}
		return limiter
	}

	delay := h.defaultRate
	if baseDelay > 0 {
		delay = baseDelay
	}

	al := &adaptiveLimiter{
		limiter: rate.NewLimiter(rate.Every(delay), h.burstSize),
	}
	actual, loaded := h.limiters.LoadOrStore(host, al)
	if loaded {
		return actual.(*adaptiveLimiter).limiter
	}
	return al.limiter
}

func (h *AdaptiveRateLimiter) ObserveLatency(host string, latency time.Duration) {
	val, ok := h.limiters.Load(host)
	if !ok {
		return
	}
	al := val.(*adaptiveLimiter)
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.lastLatency == 0 {
		al.lastLatency = latency
		return
	}

	al.lastLatency = time.Duration(
		0.7*float64(al.lastLatency) + 0.3*float64(latency),
	)

	currentRate := al.limiter.Limit()
	delay := time.Duration(float64(time.Second) / float64(currentRate))

	if al.lastLatency > delay*3 {
		newDelay := time.Duration(math.Min(float64(delay*2), float64(h.maxDelay)))
		al.limiter.SetLimit(rate.Every(newDelay))
	} else if al.lastLatency < delay/3 && delay > h.minDelay {
		newDelay := time.Duration(math.Max(float64(delay/2), float64(h.minDelay)))
		al.limiter.SetLimit(rate.Every(newDelay))
	}
}

func (h *AdaptiveRateLimiter) Wait(ctx context.Context, host string, crawlDelay time.Duration) {
	limiter := h.GetLimiter(host, crawlDelay)
	limiter.Wait(ctx)
}
