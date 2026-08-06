package crawler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

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
		headersJSON, _ := json.Marshal(a.Headers)
		gqlKey := ""
		if a.GraphQLOp != "" {
			gqlKey = a.URL + "|gql:" + a.GraphQLOp
			gqlKeyJSON, _ := json.Marshal(gqlKey)
			b.WriteString(fmt.Sprintf(`%s:{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s,"gql":%s}`,
				string(gqlKeyJSON), a.Method, a.StatusCode, string(headersJSON), string(bodyJSON), string(reqBodyJSON), string(bodyJSON)))
			b.WriteString(`,`)
		}
		b.WriteString(fmt.Sprintf(`"%s":{"m":"%s","s":%d,"h":%s,"b":%s,"rb":%s}`,
			a.URL, a.Method, a.StatusCode, string(headersJSON), string(bodyJSON), string(reqBodyJSON)))
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
