package util

import (
	"sync/atomic"
)

// Metrics holds atomic counters tracking crawl progress.
type Metrics struct {
	PagesFetched    atomic.Int64
	AssetsCaptured  atomic.Int64
	ErrorsCount     atomic.Int64
	BytesDownloaded atomic.Int64
	QueueSize       atomic.Int64
	ActiveHosts     atomic.Int64
	CircuitOpen     atomic.Int64
	CircuitHalfOpen atomic.Int64
}

// IncPagesFetched increments the pages fetched counter.
func (m *Metrics) IncPagesFetched() {
	m.PagesFetched.Add(1)
}

// IncAssetsCaptured increments the assets captured counter.
func (m *Metrics) IncAssetsCaptured() {
	m.AssetsCaptured.Add(1)
}

// IncErrors increments the error counter.
func (m *Metrics) IncErrors() {
	m.ErrorsCount.Add(1)
}

// AddBytes adds n to the total bytes downloaded counter.
func (m *Metrics) AddBytes(n int64) {
	m.BytesDownloaded.Add(n)
}

// SetQueueSize updates the current queue size.
func (m *Metrics) SetQueueSize(n int64) {
	m.QueueSize.Store(n)
}

// SetActiveHosts updates the count of active hosts.
func (m *Metrics) SetActiveHosts(n int64) {
	m.ActiveHosts.Store(n)
}

// IncCircuitOpen increments the open circuit breaker count.
func (m *Metrics) IncCircuitOpen() {
	m.CircuitOpen.Add(1)
}

// DecCircuitOpen decrements the open circuit breaker count.
func (m *Metrics) DecCircuitOpen() {
	m.CircuitOpen.Add(-1)
}

// IncCircuitHalfOpen increments the half-open circuit breaker count.
func (m *Metrics) IncCircuitHalfOpen() {
	m.CircuitHalfOpen.Add(1)
}

// DecCircuitHalfOpen decrements the half-open circuit breaker count.
func (m *Metrics) DecCircuitHalfOpen() {
	m.CircuitHalfOpen.Add(-1)
}

// Snapshot returns the current values of all counters.
func (m *Metrics) Snapshot() (pages, assets, errors, bytes int64) {
	return m.PagesFetched.Load(), m.AssetsCaptured.Load(), m.ErrorsCount.Load(), m.BytesDownloaded.Load()
}
