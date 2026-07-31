package resilience

import (
	"sync"
	"sync/atomic"
	"time"

	mysync "github.com/user/clone/internal/sync"
)

// State represents the state of a circuit breaker.
type State int32

const (
	// StateClosed indicates the circuit breaker is closed and requests are allowed.
	StateClosed State = 0
	// StateHalfOpen indicates a limited number of requests are allowed to probe recovery.
	StateHalfOpen State = 1
	// StateOpen indicates the circuit breaker is open and requests are rejected.
	StateOpen State = 2
)

// String returns the string representation of the state.
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

// CircuitBreaker tracks failures for a single host and trips open after repeated failures.
type CircuitBreaker struct {
	mu                sync.RWMutex
	state             State
	failureCount      int
	successCount      int
	failureThreshold  int
	successThreshold  int
	timeout           time.Duration
	lastFailure       time.Time
	halfOpenProbes    int32
	maxHalfOpenProbes int32
}

// HostCircuitBreaker manages a sharded set of per-host circuit breakers.
type HostCircuitBreaker struct {
	breakers      *mysync.ShardedMap[string, *CircuitBreaker]
	defaultConfig *Config
	createMu      sync.Mutex
}

// Config holds the thresholds and timeouts used to configure a circuit breaker.
type Config struct {
	FailureThreshold  int
	SuccessThreshold  int
	Timeout           time.Duration
	HalfOpenMaxProbes int
}

// DefaultConfig returns a Config populated with default circuit breaker settings.
func DefaultConfig() *Config {
	return &Config{
		FailureThreshold:  5,
		SuccessThreshold:  2,
		Timeout:           60 * time.Second,
		HalfOpenMaxProbes: 1,
	}
}

// NewHostCircuitBreaker returns a HostCircuitBreaker using default configuration.
func NewHostCircuitBreaker() *HostCircuitBreaker {
	return &HostCircuitBreaker{
		breakers:      mysync.NewShardedMap[string, *CircuitBreaker](),
		defaultConfig: DefaultConfig(),
	}
}

func (hcb *HostCircuitBreaker) getOrCreate(host string) *CircuitBreaker {
	// Fast path - try to get existing
	if cb, ok := hcb.breakers.Get(host); ok {
		return cb
	}
	// Slow path - create new (serialized)
	hcb.createMu.Lock()
	defer hcb.createMu.Unlock()
	// Double-check after acquiring lock
	if cb, ok := hcb.breakers.Get(host); ok {
		return cb
	}
	cb := NewCircuitBreaker(hcb.defaultConfig)
	hcb.breakers.Set(host, cb)
	return cb
}

// Allow reports whether a request to the given host is permitted.
func (hcb *HostCircuitBreaker) Allow(host string) bool {
	cb := hcb.getOrCreate(host)
	return cb.Allow()
}

// Success records a successful request for the given host.
func (hcb *HostCircuitBreaker) Success(host string) {
	cb := hcb.getOrCreate(host)
	cb.Success()
}

// Failure records a failed request for the given host.
func (hcb *HostCircuitBreaker) Failure(host string) {
	cb := hcb.getOrCreate(host)
	cb.Failure()
}

// State returns the current circuit breaker state for the given host.
func (hcb *HostCircuitBreaker) State(host string) State {
	cb := hcb.getOrCreate(host)
	return cb.State()
}

// Reset resets the circuit breaker for the given host back to the closed state.
func (hcb *HostCircuitBreaker) Reset(host string) {
	cb := hcb.getOrCreate(host)
	cb.Reset()
}

// Cleanup removes circuit breakers that are idle or already tripped.
func (hcb *HostCircuitBreaker) Cleanup() {
	hcb.breakers.Range(func(key string, val *CircuitBreaker) bool {
		val.mu.Lock()
		state := State(atomic.LoadInt32((*int32)(&val.state)))
		fc := val.failureCount
		val.mu.Unlock()
		if (state == StateClosed && fc == 0) || state == StateOpen || state == StateHalfOpen {
			hcb.breakers.Delete(key)
		}
		return true
	})
}

// RangeStates calls f for each circuit breaker state.
func (hcb *HostCircuitBreaker) RangeStates(f func(State)) {
	hcb.breakers.Range(func(key string, val *CircuitBreaker) bool {
		state := State(atomic.LoadInt32((*int32)(&val.state)))
		f(state)
		return true
	})
}

// NewCircuitBreaker returns a CircuitBreaker configured with the given cfg.
func NewCircuitBreaker(cfg *Config) *CircuitBreaker {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &CircuitBreaker{
		state:             StateClosed,
		failureThreshold:  cfg.FailureThreshold,
		successThreshold:  cfg.SuccessThreshold,
		timeout:           cfg.Timeout,
		halfOpenProbes:    int32(cfg.HalfOpenMaxProbes),
		maxHalfOpenProbes: int32(cfg.HalfOpenMaxProbes),
	}
}

// Allow reports whether a request may proceed through the circuit breaker.
func (cb *CircuitBreaker) Allow() bool {
	state := State(atomic.LoadInt32((*int32)(&cb.state)))
	switch state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			if atomic.CompareAndSwapInt32((*int32)(&cb.state), int32(StateOpen), int32(StateHalfOpen)) {
				atomic.StoreInt32(&cb.halfOpenProbes, int32(cb.maxHalfOpenProbes))
				return true
			}
			// CAS failed - another goroutine already transitioned, don't allow
			return false
		}
		return false
	case StateHalfOpen:
		return atomic.AddInt32(&cb.halfOpenProbes, -1) >= 0
	default:
		return false
	}
}

// Success records a successful request and resets failure counts.
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
			atomic.StoreInt32(&cb.halfOpenProbes, cb.maxHalfOpenProbes)
		}
	case StateClosed:
		cb.failureCount = 0
	}
}

// Failure records a failed request and may trip the breaker open.
func (cb *CircuitBreaker) Failure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.failureCount >= cb.failureThreshold {
		atomic.StoreInt32((*int32)(&cb.state), int32(StateOpen))
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	return State(atomic.LoadInt32((*int32)(&cb.state)))
}

// Reset returns the circuit breaker to the closed state and clears all counters.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	atomic.StoreInt32((*int32)(&cb.state), int32(StateClosed))
	cb.failureCount = 0
	cb.successCount = 0
	cb.lastFailure = time.Time{}
}
