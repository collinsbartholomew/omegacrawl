package resilience

import (
	"sync"
	"sync/atomic"
	"time"

	mysync "github.com/user/clone/internal/sync"
)

type State int32

const (
	StateClosed   State = 0
	StateHalfOpen State = 1
	StateOpen     State = 2
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

type CircuitBreaker struct {
	mu               sync.RWMutex
	state            State
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	lastFailure      time.Time
	halfOpenProbes   int32
}

type HostCircuitBreaker struct {
	breakers       *mysync.ShardedMap[string, *CircuitBreaker]
	defaultConfig  *Config
}

type Config struct {
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	HalfOpenMaxProbes int
}

func DefaultConfig() *Config {
	return &Config{
		FailureThreshold:  5,
		SuccessThreshold:  2,
		Timeout:           60 * time.Second,
		HalfOpenMaxProbes: 1,
	}
}

func NewHostCircuitBreaker() *HostCircuitBreaker {
	return &HostCircuitBreaker{
		breakers:      mysync.NewShardedMap[string, *CircuitBreaker](),
		defaultConfig: DefaultConfig(),
	}
}

func (hcb *HostCircuitBreaker) getOrCreate(host string) *CircuitBreaker {
	cb, ok := hcb.breakers.Get(host)
	if !ok {
		cb = NewCircuitBreaker(hcb.defaultConfig)
		hcb.breakers.Set(host, cb)
	}
	return cb
}

func (hcb *HostCircuitBreaker) Allow(host string) bool {
	cb := hcb.getOrCreate(host)
	return cb.Allow()
}

func (hcb *HostCircuitBreaker) Success(host string) {
	cb := hcb.getOrCreate(host)
	cb.Success()
}

func (hcb *HostCircuitBreaker) Failure(host string) {
	cb := hcb.getOrCreate(host)
	cb.Failure()
}

func (hcb *HostCircuitBreaker) State(host string) State {
	cb := hcb.getOrCreate(host)
	return cb.State()
}

func (hcb *HostCircuitBreaker) Reset(host string) {
	cb := hcb.getOrCreate(host)
	cb.Reset()
}

func (hcb *HostCircuitBreaker) Cleanup() {
	hcb.breakers.Range(func(key string, val *CircuitBreaker) bool {
		if val.State() == StateClosed && val.failureCount == 0 {
			hcb.breakers.Delete(key)
		}
		return true
	})
}

func NewCircuitBreaker(cfg *Config) *CircuitBreaker {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		halfOpenProbes:   int32(cfg.HalfOpenMaxProbes),
	}
}

func (cb *CircuitBreaker) Allow() bool {
	state := State(atomic.LoadInt32((*int32)(&cb.state)))
	switch state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			if atomic.CompareAndSwapInt32((*int32)(&cb.state), int32(StateOpen), int32(StateHalfOpen)) {
				atomic.StoreInt32(&cb.halfOpenProbes, 1)
			}
			return true
		}
		return false
	case StateHalfOpen:
		return atomic.AddInt32(&cb.halfOpenProbes, -1) >= 0
	default:
		return false
	}
}

func (cb *CircuitBreaker) Success() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := State(atomic.LoadInt32((*int32)(&cb.state)))
	switch state {
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			atomic.StoreInt32((*int32)(&cb.state), int32(StateClosed))
			cb.failureCount = 0
			cb.successCount = 0
		}
	case StateClosed:
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) Failure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.failureCount >= cb.failureThreshold {
		atomic.StoreInt32((*int32)(&cb.state), int32(StateOpen))
	}
}

func (cb *CircuitBreaker) State() State {
	return State(atomic.LoadInt32((*int32)(&cb.state)))
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	atomic.StoreInt32((*int32)(&cb.state), int32(StateClosed))
	cb.failureCount = 0
	cb.successCount = 0
	cb.lastFailure = time.Time{}
}
