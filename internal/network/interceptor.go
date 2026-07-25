package network

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

const (
	MaxResponseBodySize = 50 * 1024 * 1024
	maxRetries          = 3
)

type CapturedResource struct {
	URL        string
	Body       []byte
	MimeType   string
	StatusCode int64
	Timestamp  time.Time
	Headers    map[string]string
}

type pendingResponse struct {
	url       string
	requestID network.RequestID
	mimeType  string
	status    int64
	headers   map[string]string
	method    string
}

type APIRequest struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Body       []byte            `json:"body,omitempty"`
	Headers    map[string]string `json:"headers"`
	Timestamp  time.Time         `json:"timestamp"`
}

type APIResponse struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Body       []byte            `json:"body"`
	MimeType   string            `json:"mime_type"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Timestamp  time.Time         `json:"timestamp"`
	Size       int               `json:"size"`
	Request    *APIRequest       `json:"request,omitempty"`
}

type Interceptor struct {
	resources       map[string]*CapturedResource
	apiResponses    []APIResponse
	apiCallback     func(APIResponse)
	seen            map[string]bool
	mu              sync.RWMutex
	baseURL         string
	pending         []pendingResponse
	workerSem       chan struct{}
	pendingMethods  map[network.RequestID]string
	pendingRequests map[network.RequestID]*APIRequest
}

func NewInterceptor() *Interceptor {
	return NewInterceptorWithWorkers(10)
}

func NewInterceptorWithWorkers(workerCount int) *Interceptor {
	if workerCount < 1 {
		workerCount = 1
	}
	return &Interceptor{
		resources:       make(map[string]*CapturedResource),
		apiResponses:    make([]APIResponse, 0),
		seen:            make(map[string]bool),
		workerSem:       make(chan struct{}, workerCount),
		pendingMethods:  make(map[network.RequestID]string),
		pendingRequests: make(map[network.RequestID]*APIRequest),
	}
}

func (i *Interceptor) SetAPICallback(fn func(APIResponse)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.apiCallback = fn
}

func (i *Interceptor) Start(ctx context.Context, targetURL string) {
	i.mu.Lock()
	i.baseURL = targetURL
	i.mu.Unlock()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			i.mu.Lock()
			i.pendingMethods[e.RequestID] = e.Request.Method
			// Capture request body for non-GET requests
			if e.Request.Method != "GET" && e.Request.PostData != "" {
				i.pendingRequests[e.RequestID] = &APIRequest{
					URL:    e.Request.URL,
					Method: e.Request.Method,
					Body:   []byte(e.Request.PostData),
					Headers: func() map[string]string {
						h := make(map[string]string)
						for k, v := range e.Request.Headers {
							if s, ok := v.(string); ok {
								h[k] = s
							}
						}
						return h
					}(),
					Timestamp: time.Now(),
				}
			}
			i.mu.Unlock()
		case *network.EventResponseReceived:
			i.onResponse(e)
		case *network.EventLoadingFinished:
		}
	})

	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		util.LogDebug("network enable failed", zap.Error(err))
	}
}

func (i *Interceptor) onResponse(ev *network.EventResponseReceived) {
	respURL := ev.Response.URL
	if respURL == "" {
		return
	}

	h := make(map[string]string)
	for k, v := range ev.Response.Headers {
		if s, ok := v.(string); ok {
			h[k] = s
		}
	}

	i.mu.Lock()
			if i.seen[respURL] {
				i.mu.Unlock()
				return
			}
			i.seen[respURL] = true

			method := i.pendingMethods[ev.RequestID]
			delete(i.pendingMethods, ev.RequestID)

			delete(i.pendingRequests, ev.RequestID)

	i.pending = append(i.pending, pendingResponse{
		url:       respURL,
		requestID: ev.RequestID,
		mimeType:  ev.Response.MimeType,
		status:    ev.Response.Status,
		headers:   h,
		method:    method,
	})
	i.mu.Unlock()
}

func (i *Interceptor) FetchBodies(ctx context.Context) {
	i.mu.RLock()
	pending := make([]pendingResponse, len(i.pending))
	copy(pending, i.pending)
	i.mu.RUnlock()

	util.LogDebug("fetching response bodies via CDP", zap.Int("count", len(pending)))

	var wg sync.WaitGroup
	for _, p := range pending {
		select {
		case <-ctx.Done():
			return
		default:
		}

		wg.Add(1)
		i.workerSem <- struct{}{}
		go func(p pendingResponse) {
			defer wg.Done()
			defer func() { <-i.workerSem }()

			body := i.fetchWithRetry(ctx, p.requestID, p.url)
			if body == nil {
				return
			}

			if int64(len(body)) > MaxResponseBodySize {
				body = body[:MaxResponseBodySize]
			}

			isJSON := isJSONContentType(p.mimeType)
			isAPI := isJSON || isAPIContentType(p.url, p.mimeType)

			resource := &CapturedResource{
				URL:        p.url,
				Body:       body,
				MimeType:   p.mimeType,
				StatusCode: p.status,
				Timestamp:  time.Now(),
				Headers:    p.headers,
			}

			i.mu.Lock()
			i.resources[p.url] = resource
			if isJSON || isAPI {
				var req *APIRequest
				if r, ok := i.pendingRequests[p.requestID]; ok {
					req = r
				}
				ar := APIResponse{
					URL:        p.url,
					Body:       body,
					MimeType:   p.mimeType,
					StatusCode: int(p.status),
					Headers:    p.headers,
					Method:     p.method,
					Request:    req,
				}
				i.apiResponses = append(i.apiResponses, ar)
				if i.apiCallback != nil {
					i.apiCallback(ar)
				}
			}
			i.mu.Unlock()
		}(p)
	}
	wg.Wait()
}

func (i *Interceptor) fetchWithRetry(ctx context.Context, reqID network.RequestID, url string) []byte {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt*500) * time.Millisecond):
			case <-ctx.Done():
				return nil
			}
		}

		var body []byte
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			var err error
			body, err = network.GetResponseBody(reqID).Do(c)
			return err
		}))
		if err == nil {
			return body
		}
	}
	return nil
}

func (i *Interceptor) GetResources() map[string]*CapturedResource {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.resources
}

func (i *Interceptor) GetResource(url string) (*CapturedResource, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	r, ok := i.resources[url]
	return r, ok
}

func (i *Interceptor) GetAPIResponses() []APIResponse {
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make([]APIResponse, len(i.apiResponses))
	copy(result, i.apiResponses)
	return result
}

func (i *Interceptor) IsCaptured(url string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.seen[url]
}

func AssetPath(urlStr string, mimeType string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	ext := path.Ext(u.Path)
	if ext == "" {
		ext = extensionForMime(mimeType)
	}

	host := u.Hostname()
	cleanPath := u.Path
	if cleanPath == "" {
		cleanPath = "/index"
	}

	return path.Join(host, cleanPath)
}

func extensionForMime(mime string) string {
	switch {
	case mime == "text/html":
		return ".html"
	case mime == "text/css":
		return ".css"
	case mime == "text/javascript" || mime == "application/javascript":
		return ".js"
	case mime == "image/png":
		return ".png"
	case mime == "image/jpeg":
		return ".jpg"
	case mime == "image/gif":
		return ".gif"
	case mime == "image/svg+xml":
		return ".svg"
	case mime == "font/woff":
		return ".woff"
	case mime == "font/woff2":
		return ".woff2"
	case mime == "application/json":
		return ".json"
	default:
		return ""
	}
}

func baseMimeType(mime string) string {
	if idx := strings.IndexByte(mime, ';'); idx != -1 {
		return strings.TrimSpace(mime[:idx])
	}
	return strings.TrimSpace(mime)
}

func isJSONContentType(mime string) bool {
	base := baseMimeType(mime)
	switch base {
	case "application/json", "text/json",
		"application/vnd.api+json", "application/problem+json",
		"application/hal+json", "application/ld+json":
		return true
	}
	return false
}

func isAPIContentType(urlStr, mime string) bool {
	base := baseMimeType(mime)
	if strings.Contains(base, "xml") || strings.Contains(base, "protobuf") || strings.Contains(base, "grpc") {
		return true
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	lowPath := strings.ToLower(u.Path)
	if strings.Contains(lowPath, "/api/") || strings.Contains(lowPath, "/graphql") || strings.Contains(lowPath, "/gql") ||
		strings.Contains(lowPath, "/rest/") || strings.Contains(lowPath, "/v1/") || strings.Contains(lowPath, "/v2/") ||
		strings.Contains(lowPath, "/rpc") || strings.Contains(lowPath, "jsonrpc") {
		return true
	}
	return false
}
