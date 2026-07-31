package util

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusHandler returns an HTTP handler serving crawl metrics in the
// Prometheus text exposition format. The handler reads live values from the
// Metrics counters on each scrape, so no separate update loop is required.
func (m *Metrics) PrometheusHandler() http.Handler {
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

	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
