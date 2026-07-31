package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// CrawlStats is a snapshot of crawl progress served to the web UI.
type CrawlStats struct {
	PagesFetched int64  `json:"pages_fetched"`
	AssetsSaved  int64  `json:"assets_saved"`
	Errors       int64  `json:"errors"`
	BytesTotal   int64  `json:"bytes_total"`
	QueueSize    int    `json:"queue_size"`
	Running      bool   `json:"running"`
	Elapsed      string `json:"elapsed"`
}

// StatsProvider is implemented by components that can report crawl statistics.
type StatsProvider interface {
	Stats() CrawlStats
}

// Server serves the crawl dashboard web UI and its stats API.
type Server struct {
	provider atomic.Value
	server   *http.Server
	start    time.Time
}

// New creates a web UI server with no stats provider configured.
func New() *Server {
	return &Server{start: time.Now()}
}

// SetProvider installs the stats provider used by the stats API.
func (s *Server) SetProvider(p StatsProvider) {
	s.provider.Store(p)
}

// Start begins serving the web UI and stats API on the given port, blocking
// until the server stops.
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/stats", s.handleStats)

	s.server = &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	return s.server.ListenAndServe()
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() {
	if s.server != nil {
		s.server.Shutdown(context.Background())
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(indexHTML))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	p := s.provider.Load()
	if p == nil {
		json.NewEncoder(w).Encode(CrawlStats{Running: false})
		return
	}
	stats := p.(StatsProvider).Stats()
	stats.Elapsed = time.Since(s.start).Round(time.Second).String()
	json.NewEncoder(w).Encode(stats)
}

var indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Crawl Dashboard</title>
<style>
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family:-apple-system,BlinkMacSystemFont,sans-serif; background:#0f172a; color:#e2e8f0; padding:2rem; }
h1 { font-size:1.5rem; margin-bottom:1.5rem; color:#38bdf8; }
.stats { display:grid; grid-template-columns:repeat(auto-fit, minmax(160px,1fr)); gap:1rem; margin-bottom:2rem; }
.card { background:#1e293b; border-radius:8px; padding:1.25rem; border:1px solid #334155; }
.card .value { font-size:2rem; font-weight:700; color:#38bdf8; }
.card .label { font-size:.85rem; color:#94a3b8; margin-top:.25rem; }
#status { padding:.75rem 1rem; border-radius:6px; font-weight:600; }
#status.running { background:#166534; color:#86efac; }
#status.stopped { background:#7f1d1d; color:#fca5a5; }
</style>
</head>
<body>
<h1>Web Clone Dashboard</h1>
<div class="stats">
  <div class="card"><div class="value" id="pages">0</div><div class="label">Pages Fetched</div></div>
  <div class="card"><div class="value" id="assets">0</div><div class="label">Assets Saved</div></div>
  <div class="card"><div class="value" id="errors">0</div><div class="label">Errors</div></div>
  <div class="card"><div class="value" id="bytes">0</div><div class="label">Bytes Downloaded</div></div>
  <div class="card"><div class="value" id="queue">0</div><div class="label">Queue Size</div></div>
  <div class="card"><div class="value" id="elapsed">0s</div><div class="label">Elapsed</div></div>
</div>
<div id="status" class="running">Running...</div>
<script>
function fmt(n) { return n >= 1e9 ? (n/1e9).toFixed(1)+'G' : n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? (n/1e3).toFixed(1)+'K' : n; }
function poll() {
  fetch('/api/stats').then(r=>r.json()).then(s=>{
    document.getElementById('pages').textContent = fmt(s.pages_fetched);
    document.getElementById('assets').textContent = fmt(s.assets_saved);
    document.getElementById('errors').textContent = fmt(s.errors);
    document.getElementById('bytes').textContent = fmt(s.bytes_total);
    document.getElementById('queue').textContent = fmt(s.queue_size);
    document.getElementById('elapsed').textContent = s.elapsed;
    var st = document.getElementById('status');
    if (s.running) { st.textContent='Running...'; st.className='running'; }
    else { st.textContent='Stopped'; st.className='stopped'; }
  }).catch(()=>{});
}
setInterval(poll, 2000);
poll();
</script>
</body>
</html>`
