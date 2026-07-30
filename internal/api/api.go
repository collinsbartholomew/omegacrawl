package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type CrawlStatus struct {
	Running       bool   `json:"running"`
	PagesFetched  int64  `json:"pages_fetched"`
	AssetsSaved   int64  `json:"assets_saved"`
	Errors        int64  `json:"errors"`
	BytesTotal    int64  `json:"bytes_total"`
	QueueSize     int    `json:"queue_size"`
	Elapsed       string `json:"elapsed"`
	CurrentURL    string `json:"current_url,omitempty"`
	SeedURLs      int    `json:"seed_urls"`
}

type CrawlControl interface {
	Status() CrawlStatus
	Start(seeds []string) error
	Stop()
	Pause()
	Resume()
}

type Server struct {
	ctl     CrawlControl
	server  *http.Server
	start   time.Time
	running atomic.Bool
}

func New(ctl CrawlControl) *Server {
	return &Server{ctl: ctl, start: time.Now()}
}

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

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Stop() {
	if s.running.Load() {
		s.server.Shutdown(nil)
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

func (s *Server) json(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.ctl.Status()
	status.Elapsed = time.Since(s.start).Round(time.Second).String()
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
	if err := s.ctl.Start(req.Seeds); err != nil {
		s.json(w, map[string]string{"error": err.Error()})
		return
	}
	s.start = time.Now()
	s.json(w, map[string]string{"status": "started"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.ctl.Stop()
	s.json(w, map[string]string{"status": "stopped"})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	s.ctl.Pause()
	s.json(w, map[string]string{"status": "paused"})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	s.ctl.Resume()
	s.json(w, map[string]string{"status": "resumed"})
}
