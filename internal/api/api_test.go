package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCtl struct {
	CrawlControl
	handler http.Handler
}

func (f *fakeCtl) MetricsHandler() http.Handler { return f.handler }

func TestMetricsEndpointRegistered(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("crawler_pages_fetched_total 42"))
	})
	ctl := &fakeCtl{handler: inner}

	srv := New(ctl)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.Handle("/metrics", ctl.MetricsHandler())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "crawler_pages_fetched_total 42") {
		t.Errorf("unexpected metrics body: %q", rec.Body.String())
	}
}
