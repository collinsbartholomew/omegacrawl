package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/auth"
	"github.com/user/clone/internal/browserpool"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/changedetection"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/ratelimit"
	"github.com/user/clone/internal/resilience"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/robots"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) writeRecord(rec *storage.WARCRecord) {
	if rec == nil {
		return
	}
	if c.cfg.EnableWARC && c.warc != nil {
		if err := c.warc.WriteRecord(rec); err != nil {
			util.LogError("warc write failed", err, zap.String("url", rec.URL))
		}
	}
	if c.cfg.EnableWACZ && c.wacz != nil {
		if err := c.wacz.WriteRecord(rec); err != nil {
			util.LogError("wacz write failed", err, zap.String("url", rec.URL))
		}
	}
}

func (c *Crawler) closeWriters() {
	if c.warc != nil {
		c.warc.Close()
	}
	if c.wacz != nil {
		c.wacz.Close()
	}
}

type Crawler struct {
	cfg     *config.Config
	storage *storage.Filesystem
	warc    *storage.WARCWriter
	wacz    *storage.WACZWriter

	hostSemaphores   map[string]*hostSem
	exactDedup       *util.LRUSet
	contentHashes    *util.LRUSet
	discoveredRoutes map[string]bool
	jsErrors         *util.BoundedQueue
	wsMessages       *util.BoundedQueue
	apiResponses     *util.BoundedQueue

	browserMu     sync.Mutex
	browserCancel context.CancelFunc
	browserPool   *browserpool.Pool

	robotsParser    *robots.RobotsParser
	rewriter        *rewrite.Rewriter
	urlQueue        queue.Queue
	persistentQueue *queue.PersistentQueue
	bloomFilter     *queue.BloomDedup
	rateLimiter     *ratelimit.RateLimiter
	circuitBreaker  *resilience.HostCircuitBreaker
	retryConfig     *RetryConfig
	checkpoint      *Checkpoint
	semaphore       chan struct{}
	httpClient      *http.Client
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	hostLastCrawl   map[string]time.Time
	hostURLCount    map[string]int
	hostMu          sync.RWMutex
	routeMu         sync.RWMutex
	totalURLs       atomic.Int64
	checkpointDone  chan struct{}
	checkpointMu    sync.Mutex
	shutdown        atomic.Bool

	browserCtx context.Context
	allocOpts  []chromedp.ExecAllocatorOption

	metrics  *util.Metrics
	incCache *storage.ResourceCache

	cookieJar map[string][]*http.Cookie
	cookieMu  sync.RWMutex

	wsURLs map[network.RequestID]string
	wsMu   sync.RWMutex

	authManager    *auth.AuthManager
	changeDetector *changedetection.Detector
	captchaSolver  *captcha.Solver
	memoryBudget   *util.MemoryBudget
	excludeFn      func(string) bool
}

type hostSem struct {
	ch     chan struct{}
	closed atomic.Bool
}

func (c *Crawler) writeJSErrors() {
	errs := c.jsErrors.GetAll()
	if len(errs) == 0 {
		return
	}
	jsErrors := make([]JSError, 0, len(errs))
	for _, e := range errs {
		if je, ok := e.(JSError); ok {
			jsErrors = append(jsErrors, je)
		}
	}
	if len(jsErrors) == 0 {
		return
	}
	data, err := json.MarshalIndent(jsErrors, "", "  ")
	if err != nil {
		util.LogError("failed to marshal JS errors", err)
		return
	}
	path := c.cfg.OutputDir + "/js-errors.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write JS errors", err)
		return
	}
	util.LogInfo("wrote JS errors", zap.String("path", path), zap.Int("count", len(jsErrors)))
}

func (c *Crawler) writeWSMessages() {
	msgs := c.wsMessages.GetAll()
	if len(msgs) == 0 {
		return
	}
	wsMessages := make([]WSMsg, 0, len(msgs))
	for _, e := range msgs {
		if wm, ok := e.(WSMsg); ok {
			wsMessages = append(wsMessages, wm)
		}
	}
	if len(wsMessages) == 0 {
		return
	}
	data, err := json.MarshalIndent(wsMessages, "", "  ")
	if err != nil {
		util.LogError("failed to marshal WS messages", err)
		return
	}
	path := c.cfg.OutputDir + "/ws-messages.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write WS messages", err)
		return
	}
	util.LogInfo("wrote WS messages", zap.String("path", path), zap.Int("count", len(wsMessages)))
}

func apiURLMatches(rawURL string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if strings.HasPrefix(p, "/") {
			if u, err := url.Parse(rawURL); err == nil {
				if matched, _ := filepath.Match(p, u.Path); matched {
					return true
				}
			}
		}
		matched, _ := filepath.Match(p, rawURL)
		if matched {
			return true
		}
	}
	return false
}

func (c *Crawler) writeAPIResponses(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}
	data, err := json.MarshalIndent(apiResp, "", "  ")
	if err != nil {
		util.LogError("failed to marshal API responses", err)
		return
	}
	path := c.cfg.OutputDir + "/api-responses.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write API responses", err)
		return
	}
	util.LogInfo("wrote API responses", zap.String("path", path), zap.Int("count", len(apiResp)))
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int         `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}

type harRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	Cookies     []harNameValue `json:"cookies"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	Cookies     []harNameValue `json:"cookies"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harFile struct {
	Log harLog `json:"log"`
}

func (c *Crawler) writeHAR(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}

	entries := make([]harEntry, 0, len(apiResp))
	for _, a := range apiResp {
		headers := make([]harNameValue, 0, len(a.Headers))
		for k, v := range a.Headers {
			headers = append(headers, harNameValue{Name: k, Value: v})
		}
		var cookies []harNameValue
		for k, v := range a.Headers {
			if strings.ToLower(k) == "set-cookie" || strings.ToLower(k) == "cookie" {
				cookies = append(cookies, harNameValue{Name: k, Value: v})
			}
		}
		if cookies == nil {
			cookies = []harNameValue{}
		}

		statusText := http.StatusText(a.StatusCode)
		if statusText == "" {
			statusText = "Unknown"
		}

		contentType := "application/octet-stream"
		if ct, ok := a.Headers["Content-Type"]; ok {
			contentType = ct
		} else if ct, ok := a.Headers["content-type"]; ok {
			contentType = ct
		}

		mimeType := contentType
		if idx := strings.IndexByte(contentType, ';'); idx != -1 {
			mimeType = strings.TrimSpace(contentType[:idx])
		}

		bodySize := a.Size
		headersSize := 0
		for _, h := range headers {
			headersSize += len(h.Name) + len(h.Value) + 4
		}

		entries = append(entries, harEntry{
			StartedDateTime: a.Timestamp.Format(time.RFC3339),
			Time:            -1,
			Request: harRequest{
				Method:      a.Method,
				URL:         a.URL,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				QueryString: []harNameValue{},
				Cookies:     cookies,
				HeadersSize: headersSize,
				BodySize:    bodySize,
			},
			Response: harResponse{
				Status:      a.StatusCode,
				StatusText:  statusText,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				Cookies:     cookies,
				Content: harContent{
					Size:     bodySize,
					MimeType: mimeType,
					Text:     string(a.Body),
				},
				RedirectURL: "",
				HeadersSize: headersSize,
				BodySize:    bodySize,
			},
			Cache: struct{}{},
			Timings: harTimings{
				Send:    -1,
				Wait:    -1,
				Receive: -1,
			},
		})
	}

	har := harFile{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{
				Name:    "clone",
				Version: "1.0",
			},
			Entries: entries,
		},
	}

	data, err := json.MarshalIndent(har, "", "  ")
	if err != nil {
		util.LogError("failed to marshal HAR", err)
		return
	}

	path := c.cfg.OutputDir + "/api-responses.har"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write HAR", err)
		return
	}
	util.LogInfo("wrote HAR", zap.String("path", path), zap.Int("count", len(entries)))
}

func (c *Crawler) writeSW(responses []interface{}, wsRaw []interface{}) {
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}

	wsByURL := make(map[string][]WSMsg)
	for _, w := range wsRaw {
		if m, ok := w.(WSMsg); ok && m.URL != "" {
			wsByURL[m.URL] = append(wsByURL[m.URL], m)
		}
	}
	wsJSON, _ := json.Marshal(wsByURL)

	urlMappings := c.rewriter.GetMappings()
	urlMapRel := make(map[string]string, len(urlMappings))
	outPrefix := c.cfg.OutputDir
	if !strings.HasSuffix(outPrefix, "/") {
		outPrefix += "/"
	}
	for origURL, localPath := range urlMappings {
		rel := strings.TrimPrefix(localPath, outPrefix)
		rel = filepath.ToSlash(rel)
		urlMapRel[origURL] = rel
	}
	urlMapJSON, _ := json.Marshal(urlMapRel)

	var b strings.Builder
	b.WriteString(`const CACHE = 'clone-v1';
const API_MAP = {`)
	for i, a := range apiResp {
		if i > 0 {
			b.WriteString(`,`)
		}
		bodyJSON, _ := json.Marshal(string(a.Body))
		reqBodyJSON, _ := json.Marshal(string(a.RequestBody))
		gqlKey := ""
		if a.GraphQLOp != "" {
			gqlKey = a.URL + "|gql:" + a.GraphQLOp
			gqlKeyJSON, _ := json.Marshal(gqlKey)
			b.WriteString(fmt.Sprintf(`%s:{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s,"gql":%s}`,
				string(gqlKeyJSON), a.Method, a.StatusCode, jsonHeadrs(a.Headers), string(bodyJSON), string(reqBodyJSON), string(bodyJSON)))
			b.WriteString(`,`)
		}
		b.WriteString(fmt.Sprintf(`"%s":{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s}`,
			a.URL, a.Method, a.StatusCode, jsonHeadrs(a.Headers), string(bodyJSON), string(reqBodyJSON)))
	}
	b.WriteString(`};
const WS_MAP = `)
	b.Write(wsJSON)
	b.WriteString(`;
const URL_MAP = `)
	b.Write(urlMapJSON)
	b.WriteString(`;
const STATIC_EXT = /\.(css|js|png|jpg|jpeg|gif|svg|ico|woff2?|ttf|eot|webp)(\?.*)?$/;
const API_PATTERN = /\/api\//;

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE).then(function(cache) {
      var urls = Object.keys(URL_MAP).map(function(url) { return URL_MAP[url]; });
      return Promise.all(
        urls.map(function(path) {
          return cache.add(path).catch(function() {});
        })
      );
    }).then(function() {
      self.skipWaiting();
    })
  );
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys =>
    Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
  ));
  return self.clients.claim();
});

function matchAPI(url, method, body) {
  if (isGraphQL(url) && body) {
    try {
      var parsed = JSON.parse(body);
      if (parsed.operationName) {
        var gqlKey = url + '|gql:' + parsed.operationName;
        var gqlEntry = API_MAP[gqlKey];
        if (gqlEntry && (!gqlEntry.m || gqlEntry.m === method)) return gqlEntry;
      }
    } catch(e) {}
  }
  const entry = API_MAP[url];
  if (entry && (!entry.m || entry.m === method)) return entry;
  const noQuery = url.split('?')[0].split('#')[0];
  if (noQuery !== url) {
    const e2 = API_MAP[noQuery];
    if (e2 && (!e2.m || e2.m === method)) return e2;
  }
  for (const [key, val] of Object.entries(API_MAP)) {
    if (key.includes('|gql:')) continue;
    const base = key.split('?')[0];
    if (base === noQuery && (!val.m || val.m === method)) return val;
  }
  return null;
}

function isGraphQL(url) {
  return url.includes('/graphql') || url.includes('/gql');
}

function getWSMessages(url) {
  const normalized = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
  let msgs = WS_MAP[url] || WS_MAP[normalized];
  if (msgs) return msgs;
  for (const [key, val] of Object.entries(WS_MAP)) {
    if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === normalized) return val;
  }
  return null;
}

function matchURL(url) {
  const withoutQuery = url.split('?')[0].split('#')[0];
  if (URL_MAP[url]) return URL_MAP[url];
  if (URL_MAP[withoutQuery]) return URL_MAP[withoutQuery];
  for (const [key, val] of Object.entries(URL_MAP)) {
    if (key.split('?')[0].split('#')[0] === withoutQuery) return val;
  }
  return null;
}

function isHTML(url) {
  return !STATIC_EXT.test(url) && !API_PATTERN.test(url) && !url.includes('/api/');
}

self.addEventListener('fetch', event => {
  const { request } = event;
  const url = request.url;

  if (API_PATTERN.test(url) || isGraphQL(url)) {
    if (isGraphQL(url) && (request.method === 'POST' || request.method === 'PUT')) {
      event.respondWith(
        request.clone().text().then(function(body) {
          var entry = matchAPI(url, request.method, body);
          if (entry) {
            if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
              var replay = new Request(url, {
                method: request.method,
                headers: request.headers,
                body: entry.rb
              });
              return fetch(replay)['catch'](function() {
                return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
              });
            }
            return new Response(entry.b, { status: entry.s, statusText: 'OK', headers: entry.h });
          }
          return fetch(request)['catch'](function() {
            return new Response('', { status: 503 });
          });
        })
      );
      return;
    }
    const entry = matchAPI(url, request.method, null);
    if (entry) {
      if (entry.rb && (request.method === 'POST' || request.method === 'PATCH' || request.method === 'PUT')) {
        const replay = new Request(url, {
          method: request.method,
          headers: request.headers,
          body: entry.rb
        });
        event.respondWith(
          fetch(replay).catch(() => new Response(entry.b, {
            status: entry.s,
            statusText: 'OK',
            headers: entry.h
          }))
        );
        return;
      }
      event.respondWith(new Response(entry.b, {
        status: entry.s,
        statusText: 'OK',
        headers: entry.h
      }));
      return;
    }
  }

  if (STATIC_EXT.test(url)) {
    event.respondWith(
      caches.match(request).then(cached => {
        if (cached) return cached;
        return fetch(request).then(res => {
          const copy = res.clone();
          caches.open(CACHE).then(cache => cache.put(request, copy));
          return res;
        }).catch(async () => {
          const localPath = matchURL(url);
          if (localPath) {
            const cached = await caches.match(localPath);
            if (cached) return cached;
          }
          return new Response('', { status: 404 });
        });
      })
    );
    return;
  }

  if (isHTML(url)) {
    event.respondWith(
      fetch(request).then(res => {
        const copy = res.clone();
        caches.open(CACHE).then(cache => {
          if (copy.ok && copy.headers.get('content-type')?.includes('text/html')) {
            cache.put(request, copy);
          }
        });
        return res;
      }).catch(async () => {
        const cached = await caches.match(request);
        if (cached) return cached;
        const localPath = matchURL(url);
        if (localPath) {
          const localCached = await caches.match(localPath);
          if (localCached) return localCached;
          return fetch(localPath).catch(() => caches.match('/offline.html'));
        }
        return new Response('Offline', { status: 503 });
      })
    );
    return;
  }

  event.respondWith(fetch(request));
});`)

	path := c.cfg.OutputDir + "/sw.js"
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		util.LogError("failed to write sw.js", err)
		return
	}
	util.LogInfo("wrote sw.js", zap.String("path", path), zap.Int("api_count", len(apiResp)))

	wsReplayScript := `(function() {
	var wsData = null;
	fetch('ws-data.json').then(function(r) { return r.json(); }).then(function(data) { wsData = data; }).catch(function() {});
	var NativeWebSocket = window.WebSocket;
	function findMessages(url) {
		if (!wsData) return null;
		var httpURL = url.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:');
		if (wsData[httpURL]) return wsData[httpURL];
		if (wsData[url]) return wsData[url];
		for (var key in wsData) {
			if (key.replace(/^ws:/, 'http:').replace(/^wss:/, 'https:') === httpURL) return wsData[key];
		}
		return null;
	}
	function decodeData(msg) {
		if (msg.is_binary) {
			var binaryStr = atob(msg.data);
			var bytes = new Uint8Array(binaryStr.length);
			for (var i = 0; i < binaryStr.length; i++) {
				bytes[i] = binaryStr.charCodeAt(i);
			}
			return bytes.buffer;
		}
		return msg.data;
	}
	window.WebSocket = function(url, protocols) {
		var msgs = findMessages(url);
		if (msgs) {
			var ws = {
				url: url, readyState: 0,
				CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3,
				onopen: null, onclose: null, onmessage: null, onerror: null,
				bufferedAmount: 0, extensions: '', protocol: protocols || '',
				close: function() { this.readyState = 3; if (this.onclose) this.onclose({ code: 1000, reason: 'Replay complete', wasClean: true }); },
				send: function(data) {},
				addEventListener: function(type, listener) {
					if (type === 'open') this.onopen = listener;
					else if (type === 'message') this.onmessage = listener;
					else if (type === 'close') this.onclose = listener;
					else if (type === 'error') this.onerror = listener;
				}
			};
			ws.readyState = 0;
			setTimeout(function() {
				ws.readyState = 1;
				if (ws.onopen) ws.onopen({ target: ws });
				var receives = [];
				var timestamps = [];
				var baseTime = 0;
				for (var i = 0; i < msgs.length; i++) {
					if (msgs[i].direction === 'receive') {
						receives.push(msgs[i]);
						timestamps.push(msgs[i].timestamp ? new Date(msgs[i].timestamp).getTime() : 0);
					}
				}
				if (timestamps.length > 0 && timestamps[0] > 0) {
					baseTime = timestamps[0];
				}
				for (var i = 0; i < receives.length; i++) {
					(function(idx, msg) {
						var delay = 50;
						if (baseTime > 0 && timestamps[idx] > 0) {
							delay = timestamps[idx] - baseTime;
						} else {
							delay = idx * 50;
						}
						setTimeout(function() {
							if (ws.onmessage) ws.onmessage({ data: decodeData(msg), target: ws, type: 'message' });
						}, delay);
					})(i, receives[i]);
				}
				var lastDelay = receives.length > 0 ? (timestamps[receives.length-1] > 0 ? timestamps[receives.length-1] - baseTime : receives.length * 50) + 100 : 100;
				setTimeout(function() {
					ws.readyState = 3;
					if (ws.onclose) ws.onclose({ code: 1000, reason: 'Replay complete', wasClean: true });
				}, Math.max(lastDelay, 100));
			}, 100);
			return ws;
		}
		return new NativeWebSocket(url, protocols);
	};
	window.WebSocket.CONNECTING = 0;
	window.WebSocket.OPEN = 1;
	window.WebSocket.CLOSING = 2;
	window.WebSocket.CLOSED = 3;
})();`

	wsReplayPath := c.cfg.OutputDir + "/ws-replay.js"
	if err := os.WriteFile(wsReplayPath, []byte(wsReplayScript), 0644); err != nil {
		util.LogError("failed to write ws-replay.js", err)
	} else {
		util.LogInfo("wrote ws-replay.js", zap.String("path", wsReplayPath))
	}

	wsDataPath := c.cfg.OutputDir + "/ws-data.json"
	if wsData, err := json.MarshalIndent(wsByURL, "", "  "); err == nil {
		if err := os.WriteFile(wsDataPath, wsData, 0644); err != nil {
			util.LogError("failed to write ws-data.json", err)
		} else {
			util.LogInfo("wrote ws-data.json", zap.String("path", wsDataPath), zap.Int("ws_urls", len(wsByURL)))
		}
	}
}

func (c *Crawler) writeSitemap() {
	c.routeMu.RLock()
	urls := make([]string, 0, len(c.discoveredRoutes))
	for u := range c.discoveredRoutes {
		urls = append(urls, u)
	}
	c.routeMu.RUnlock()
	if len(urls) == 0 {
		return
	}
	sort.Strings(urls)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range urls {
		escaped := strings.ReplaceAll(u, "&", "&amp;")
		escaped = strings.ReplaceAll(escaped, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		b.WriteString(fmt.Sprintf("  <url><loc>%s</loc></url>\n", escaped))
	}
	b.WriteString("</urlset>\n")
	path := c.cfg.OutputDir + "/sitemap.xml"
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		util.LogError("failed to write sitemap.xml", err)
		return
	}
	util.LogInfo("wrote sitemap", zap.String("path", path), zap.Int("urls", len(urls)))
}

func extractGraphQLOp(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var gqlReq struct {
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(body, &gqlReq); err != nil {
		return ""
	}
	return gqlReq.OperationName
}

func jsonHeadrs(h map[string]string) string {
	if len(h) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range h {
		if !first {
			b.WriteByte(',')
		}
		first = false
		kk, _ := json.Marshal(k)
		vv, _ := json.Marshal(v)
		b.WriteString(fmt.Sprintf("%s:%s", string(kk), string(vv)))
	}
	b.WriteByte('}')
	return b.String()
}
