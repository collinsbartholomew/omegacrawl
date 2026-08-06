package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// MetricsHandler serves metrics in the Prometheus text exposition format.
type MetricsHandler interface {
	// MetricsHandler returns an HTTP handler serving scrape metrics.
	MetricsHandler() http.Handler
}

// CrawlStatus reports the current state of a crawl operation.
type CrawlStatus struct {
	Running      bool   `json:"running"`
	Paused       bool   `json:"paused"`
	PagesFetched int64  `json:"pages_fetched"`
	AssetsSaved  int64  `json:"assets_saved"`
	Errors       int64  `json:"errors"`
	BytesTotal   int64  `json:"bytes_total"`
	QueueSize    int    `json:"queue_size"`
	Elapsed      string `json:"elapsed"`
	CurrentURL   string `json:"current_url,omitempty"`
	SeedURLs     int    `json:"seed_urls"`
}

// HealthStatus represents the health of the service.
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// CrawlControl defines the interface for controlling a crawl lifecycle.
type CrawlControl interface {
	Status() CrawlStatus
	IsRunning() bool
	Start(seeds []string) error
	Stop()
	Pause()
	Resume()
}

// Server serves the crawl control HTTP API.
type Server struct {
	ctl        CrawlControl
	server     *http.Server
	start      time.Time
	running    atomic.Bool
	mu         sync.Mutex
	active     bool
	rateLimiter *rate.Limiter
}

// New creates a new API Server backed by the given crawl controller.
func New(ctl CrawlControl) *Server {
	return &Server{
		ctl:         ctl,
		start:       time.Now(),
		rateLimiter: rate.NewLimiter(rate.Limit(100), 200), // 100 req/s, burst 200
	}
}

// Start begins serving the HTTP API on the given port.
func (s *Server) Start(port int) error {
	if !s.running.CompareAndSwap(false, true) {
		return fmt.Errorf("server already running")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/pause", s.handlePause)
	mux.HandleFunc("/api/resume", s.handleResume)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	if mh, ok := s.ctl.(MetricsHandler); ok {
		mux.Handle("/metrics", mh.MetricsHandler())
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      corsMiddleware(rateLimitMiddleware(s.rateLimiter)(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return s.server.ListenAndServe()
}

// Stop shuts down the HTTP server if it is running.
func (s *Server) Stop() {
	if s.running.Load() {
		s.server.Shutdown(context.Background())
		s.running.Store(false)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(limiter *rate.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health checks
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) json(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := s.ctl.Status()
	s.mu.Lock()
	elapsed := time.Since(s.start).Round(time.Second).String()
	s.mu.Unlock()
	status.Elapsed = elapsed
	s.json(w, status)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Seeds []string `json:"seeds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Seeds = nil
	}
	if len(req.Seeds) == 0 {
		http.Error(w, "seeds required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.active || s.ctl.IsRunning() {
		s.mu.Unlock()
		http.Error(w, "crawl already running", http.StatusConflict)
		return
	}
	s.active = true
	s.start = time.Now()
	s.mu.Unlock()
	s.json(w, map[string]string{"status": "started"})

	// Run crawl asynchronously
	go func() {
		if err := s.ctl.Start(req.Seeds); err != nil {
			// Error logged by crawler
		}
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
	}()
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.ctl.Stop()
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
	s.json(w, map[string]string{"status": "stopped"})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.ctl.IsRunning() {
		http.Error(w, "no crawl running", http.StatusConflict)
		return
	}
	s.ctl.Pause()
	s.json(w, map[string]string{"status": "paused"})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.ctl.IsRunning() {
		http.Error(w, "no crawl running", http.StatusConflict)
		return
	}
	s.ctl.Resume()
	s.json(w, map[string]string{"status": "resumed"})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	health := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Checks: map[string]string{
			"api": "ok",
		},
	}
	
	// Add crawler health if available
	if s.ctl != nil {
		if s.ctl.IsRunning() {
			health.Checks["crawler"] = "running"
		} else {
			health.Checks["crawler"] = "idle"
		}
	}
	
	s.json(w, health)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Check if server is ready to accept requests
	ready := HealthStatus{
		Status:    "ready",
		Timestamp: time.Now(),
		Checks: map[string]string{
			"api": "ready",
		},
	}
	
	// If a crawl is starting up, might not be ready
	s.mu.Lock()
	if s.active && !s.ctl.IsRunning() {
		ready.Status = "not_ready"
		ready.Checks["crawler"] = "starting"
	} else {
		ready.Checks["crawler"] = "ready"
	}
	s.mu.Unlock()
	
	statusCode := http.StatusOK
	if ready.Status == "not_ready" {
		statusCode = http.StatusServiceUnavailable
	}
	
	w.WriteHeader(statusCode)
	s.json(w, ready)
}
