package util

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	handlerOnce sync.Once
	handler     http.Handler
}

// PrometheusHandler returns an HTTP handler serving crawl metrics in the
// Prometheus text exposition format. The handler reads live values from the
// Metrics counters on each scrape, so no separate update loop is required. The
// handler and its registry are built once and cached; repeated calls return the
// same handler so multiple scrapes never register duplicate collectors.
func (m *Metrics) PrometheusHandler() http.Handler {
	m.handlerOnce.Do(func() {
		reg := prometheus.NewRegistry()

		c := prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "crawler",
			Name:      "pages_fetched_total",
			Help:      "Total number of pages fetched.",
		}, func() float64 { return float64(m.PagesFetched.Load()) })
		reg.MustRegister(c)

		a := prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "crawler",
			Name:      "assets_captured_total",
			Help:      "Total number of assets captured.",
		}, func() float64 { return float64(m.AssetsCaptured.Load()) })
		reg.MustRegister(a)

		e := prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "crawler",
			Name:      "errors_total",
			Help:      "Total number of crawl errors.",
		}, func() float64 { return float64(m.ErrorsCount.Load()) })
		reg.MustRegister(e)

		b := prometheus.NewCounterFunc(prometheus.CounterOpts{
			Namespace: "crawler",
			Name:      "bytes_downloaded_total",
			Help:      "Total number of bytes downloaded.",
		}, func() float64 { return float64(m.BytesDownloaded.Load()) })
		reg.MustRegister(b)

		q := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "crawler",
			Name:      "queue_size",
			Help:      "Current size of the crawl queue.",
		}, func() float64 { return float64(m.QueueSize.Load()) })
		reg.MustRegister(q)

		ah := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "crawler",
			Name:      "active_hosts",
			Help:      "Number of hosts currently being crawled.",
		}, func() float64 { return float64(m.ActiveHosts.Load()) })
		reg.MustRegister(ah)

		co := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "crawler",
			Name:      "circuit_breakers_open",
			Help:      "Number of circuit breakers in open state.",
		}, func() float64 { return float64(m.CircuitOpen.Load()) })
		reg.MustRegister(co)

		ch := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "crawler",
			Name:      "circuit_breakers_half_open",
			Help:      "Number of circuit breakers in half-open state.",
		}, func() float64 { return float64(m.CircuitHalfOpen.Load()) })
		reg.MustRegister(ch)

		reg.MustRegister(collectors.NewGoCollector())

		m.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	})
	return m.handler
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

// ResetCircuitOpen zeroes the open circuit breaker gauge before a recount.
func (m *Metrics) ResetCircuitOpen() {
	m.CircuitOpen.Store(0)
}

// DecCircuitOpen decrements the open circuit breaker count.
func (m *Metrics) DecCircuitOpen() {
	m.CircuitOpen.Add(-1)
}

// IncCircuitHalfOpen increments the half-open circuit breaker count.
func (m *Metrics) IncCircuitHalfOpen() {
	m.CircuitHalfOpen.Add(1)
}

// ResetCircuitHalfOpen zeroes the half-open circuit breaker gauge before a
// recount.
func (m *Metrics) ResetCircuitHalfOpen() {
	m.CircuitHalfOpen.Store(0)
}

// DecCircuitHalfOpen decrements the half-open circuit breaker count.
func (m *Metrics) DecCircuitHalfOpen() {
	m.CircuitHalfOpen.Add(-1)
}

// Snapshot returns the current values of all counters.
func (m *Metrics) Snapshot() (pages, assets, errors, bytes int64) {
	return m.PagesFetched.Load(), m.AssetsCaptured.Load(), m.ErrorsCount.Load(), m.BytesDownloaded.Load()
}
