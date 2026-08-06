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

// recordingCtl records control calls so handlers can be asserted against.
type recordingCtl struct {
	paused  bool
	stopped bool
}

func (r *recordingCtl) Status() CrawlStatus {
	return CrawlStatus{Running: !r.stopped, Paused: r.paused}
}

func (r *recordingCtl) IsRunning() bool { return !r.stopped }
func (r *recordingCtl) Start(seeds []string) error {
	return nil
}
func (r *recordingCtl) Stop()              { r.stopped = true; r.paused = false }
func (r *recordingCtl) Pause()             { r.paused = true }
func (r *recordingCtl) Resume()            { r.paused = false }

func TestPauseResumeHandlers(t *testing.T) {
	ctl := &recordingCtl{}
	srv := New(ctl)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pause", srv.handlePause)
	mux.HandleFunc("/api/resume", srv.handleResume)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/pause", nil))
	if rec.Code != 200 {
		t.Fatalf("pause: expected 200, got %d", rec.Code)
	}
	if !ctl.paused {
		t.Error("pause handler did not pause the controller")
	}
	if !strings.Contains(rec.Body.String(), "paused") {
		t.Errorf("pause response should mention paused, got %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/resume", nil))
	if rec.Code != 200 {
		t.Fatalf("resume: expected 200, got %d", rec.Code)
	}
	if ctl.paused {
		t.Error("resume handler did not resume the controller")
	}

	// GET must be rejected.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/pause", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("pause GET: expected 405, got %d", rec.Code)
	}
}

func TestStatusIncludesPaused(t *testing.T) {
	ctl := &recordingCtl{}
	srv := New(ctl)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	if rec.Code != 200 {
		t.Fatalf("status: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"paused":false`) {
		t.Errorf("status should report paused=false, got %q", rec.Body.String())
	}

	ctl.paused = true
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	if !strings.Contains(rec.Body.String(), `"paused":true`) {
		t.Errorf("status should report paused=true, got %q", rec.Body.String())
	}
}
