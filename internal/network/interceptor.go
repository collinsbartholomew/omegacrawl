package network

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
)

const (
	// MaxResponseBodySize is the maximum size of a captured response body in bytes.
	MaxResponseBodySize = 50 * 1024 * 1024
	maxRetries          = 3
	maxSeenURLs         = 200000
	maxResources        = 50000
	maxAPIResponses     = 10000
)

// CapturedResource is a captured network response for a single URL.
type CapturedResource struct {
	URL        string
	Body       []byte
	MimeType   string
	StatusCode int64
	Timestamp  time.Time
	Headers    map[string]string
}

// APIRequest is a captured outbound API request.
type APIRequest struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Body      []byte            `json:"body,omitempty"`
	Headers   map[string]string `json:"headers"`
	Timestamp time.Time         `json:"timestamp"`
}

// APIResponse is a captured API response with its metadata.
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

type pendingResource struct {
	requestID network.RequestID
	url       string
	mimeType  string
	status    int64
	headers   map[string]string
	method    string
	request   *APIRequest
}

// Interceptor captures network traffic from a Chromium session via CDP.
type Interceptor struct {
	mu              sync.RWMutex
	resources       map[string]*CapturedResource
	apiResponses    []APIResponse
	apiCallback     func(APIResponse)
	seen            *util.LRUSet
	pending         map[network.RequestID]*pendingResource
	pendingMethods  map[network.RequestID]string
	pendingRequests map[network.RequestID]*APIRequest
	workerSem       chan struct{}
	baseURL         string
	fetchWg         sync.WaitGroup
	fetchCtx        context.Context
	fetchCancel     context.CancelFunc
}

// NewInterceptor creates an Interceptor with 10 worker goroutines.
func NewInterceptor() *Interceptor {
	return NewInterceptorWithWorkers(10)
}

// NewInterceptorWithWorkers creates an Interceptor with the given number of worker goroutines.
func NewInterceptorWithWorkers(workerCount int) *Interceptor {
	if workerCount < 1 {
		workerCount = 1
	}
	return &Interceptor{
		resources:       make(map[string]*CapturedResource),
		apiResponses:    make([]APIResponse, 0, maxAPIResponses),
		seen:            util.NewLRUSet(maxSeenURLs),
		pending:         make(map[network.RequestID]*pendingResource),
		pendingMethods:  make(map[network.RequestID]string),
		pendingRequests: make(map[network.RequestID]*APIRequest),
		workerSem:       make(chan struct{}, workerCount),
	}
}

// SetAPICallback sets the callback invoked when an API response is captured.
func (i *Interceptor) SetAPICallback(fn func(APIResponse)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.apiCallback = fn
}

// Start attaches the interceptor to the Chromium session for the given targetURL.
func (i *Interceptor) Start(ctx context.Context, targetURL string) {
	i.mu.Lock()
	i.baseURL = targetURL
	if i.fetchCancel != nil {
		i.fetchCancel()
	}
	i.fetchCtx, i.fetchCancel = context.WithCancel(ctx)
	i.mu.Unlock()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			i.onRequest(e)
		case *network.EventResponseReceived:
			i.onResponse(e)
		case *network.EventLoadingFinished:
			i.onLoadingFinished(ctx, e)
		}
	})

	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		util.LogDebug("network enable failed", zap.Error(err))
	}
}

func headerString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (i *Interceptor) onRequest(ev *network.EventRequestWillBeSent) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.pendingMethods[ev.RequestID] = ev.Request.Method

	if ev.Request.Method != "GET" && ev.Request.PostData != "" {
		i.pendingRequests[ev.RequestID] = &APIRequest{
			URL:    ev.Request.URL,
			Method: ev.Request.Method,
			Body:   []byte(ev.Request.PostData),
			Headers: func() map[string]string {
				h := make(map[string]string)
				for k, v := range ev.Request.Headers {
					h[k] = headerString(v)
				}
				return h
			}(),
			Timestamp: time.Now(),
		}
	}
}

func (i *Interceptor) onResponse(ev *network.EventResponseReceived) {
	respURL := ev.Response.URL
	if respURL == "" {
		return
	}

	h := make(map[string]string)
	for k, v := range ev.Response.Headers {
		h[k] = headerString(v)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.seen.Contains(respURL) {
		return
	}
	i.seen.Add(respURL)

	method := i.pendingMethods[ev.RequestID]
	delete(i.pendingMethods, ev.RequestID)

	req := i.pendingRequests[ev.RequestID]
	delete(i.pendingRequests, ev.RequestID)

	i.pending[ev.RequestID] = &pendingResource{
		requestID: ev.RequestID,
		url:       respURL,
		mimeType:  ev.Response.MimeType,
		status:    ev.Response.Status,
		headers:   h,
		method:    method,
		request:   req,
	}
}

func (i *Interceptor) onLoadingFinished(ctx context.Context, ev *network.EventLoadingFinished) {
	i.mu.Lock()
	p, ok := i.pending[ev.RequestID]
	if !ok {
		i.mu.Unlock()
		return
	}
	delete(i.pending, ev.RequestID)
	i.fetchWg.Add(1)
	i.mu.Unlock()

	go func() {
		defer i.fetchWg.Done()
		select {
		case i.workerSem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-i.workerSem }()
		i.fetchAndProcess(ctx, p)
	}()
}

func (i *Interceptor) fetchAndProcess(ctx context.Context, p *pendingResource) {
	body := i.fetchWithRetry(ctx, p.requestID, p.url)
	if body == nil {
		return
	}

	if int64(len(body)) > MaxResponseBodySize {
		body = body[:MaxResponseBodySize]
	}

	resource := &CapturedResource{
		URL:        p.url,
		Body:       body,
		MimeType:   p.mimeType,
		StatusCode: p.status,
		Timestamp:  time.Now(),
		Headers:    p.headers,
	}

	isJSON := isJSONContentType(p.mimeType) || isAPIContentType(p.url, p.mimeType)

	var ar APIResponse
	i.mu.Lock()
	if len(i.resources) < maxResources {
		i.resources[p.url] = resource
	}

	if isJSON {
		ar = APIResponse{
			URL:        p.url,
			Body:       body,
			MimeType:   p.mimeType,
			StatusCode: int(p.status),
			Headers:    p.headers,
			Method:     p.method,
			Timestamp:  time.Now(),
			Size:       len(body),
			Request:    p.request,
		}
		if len(i.apiResponses) < maxAPIResponses {
			i.apiResponses = append(i.apiResponses, ar)
		}
	}
	i.mu.Unlock()
	if isJSON && i.apiCallback != nil {
		i.apiCallback(ar)
	}
}

func (i *Interceptor) fetchWithRetry(ctx context.Context, reqID network.RequestID, url string) []byte {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			base := time.Duration(attempt*300) * time.Millisecond
			jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
			retryTimer := time.NewTimer(base + jitter)
			select {
			case <-retryTimer.C:
			case <-ctx.Done():
				if !retryTimer.Stop() {
					<-retryTimer.C
				}
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

// FetchBodies fetches the remaining response bodies via CDP.
func (i *Interceptor) FetchBodies(ctx context.Context) {
	i.fetchWg.Wait()

	i.mu.Lock()
	remaining := make([]*pendingResource, 0, len(i.pending))
	for _, p := range i.pending {
		remaining = append(remaining, p)
	}
	i.pending = make(map[network.RequestID]*pendingResource)
	i.mu.Unlock()

	if len(remaining) == 0 {
		return
	}

	util.LogDebug("fetching remaining response bodies via CDP", zap.Int("count", len(remaining)))

	fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)
	defer fetchCancel()

	var wg sync.WaitGroup
	for _, p := range remaining {
		wg.Add(1)
		select {
		case i.workerSem <- struct{}{}:
		case <-fetchCtx.Done():
			wg.Done()
			continue
		}
		go func(pr *pendingResource) {
			defer wg.Done()
			defer func() { <-i.workerSem }()

			body := i.fetchWithRetry(fetchCtx, pr.requestID, pr.url)
			if body == nil {
				return
			}

			if int64(len(body)) > MaxResponseBodySize {
				body = body[:MaxResponseBodySize]
			}

			resource := &CapturedResource{
				URL:        pr.url,
				Body:       body,
				MimeType:   pr.mimeType,
				StatusCode: pr.status,
				Timestamp:  time.Now(),
				Headers:    pr.headers,
			}

			isJSON := isJSONContentType(pr.mimeType) || isAPIContentType(pr.url, pr.mimeType)

			var ar APIResponse
			i.mu.Lock()
			if len(i.resources) < maxResources {
				i.resources[pr.url] = resource
			}

			if isJSON {
				ar = APIResponse{
					URL:        pr.url,
					Body:       body,
					MimeType:   pr.mimeType,
					StatusCode: int(pr.status),
					Headers:    pr.headers,
					Method:     pr.method,
					Timestamp:  time.Now(),
					Size:       len(body),
					Request:    pr.request,
				}
				if len(i.apiResponses) < maxAPIResponses {
					i.apiResponses = append(i.apiResponses, ar)
				}
			}
			i.mu.Unlock()
			if isJSON && i.apiCallback != nil {
				i.apiCallback(ar)
			}
		}(p)
	}
	wg.Wait()
}

// GetResources returns a copy of the captured resources keyed by URL.
func (i *Interceptor) GetResources() map[string]*CapturedResource {
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make(map[string]*CapturedResource, len(i.resources))
	for k, v := range i.resources {
		result[k] = v
	}
	return result
}

// GetResource returns the captured resource for the given URL, if any.
func (i *Interceptor) GetResource(url string) (*CapturedResource, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	r, ok := i.resources[url]
	return r, ok
}

// GetAPIResponses returns a copy of the captured API responses.
func (i *Interceptor) GetAPIResponses() []APIResponse {
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make([]APIResponse, len(i.apiResponses))
	copy(result, i.apiResponses)
	return result
}

// IsCaptured reports whether the URL was seen during the session.
func (i *Interceptor) IsCaptured(url string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.seen.Contains(url)
}

// DownloadResourceViaHTTP fetches rawURL over HTTP and returns it as a CapturedResource.
func (i *Interceptor) DownloadResourceViaHTTP(rawURL string) (*CapturedResource, error) {
	i.mu.RLock()
	baseURL := i.baseURL
	i.mu.RUnlock()
	if baseURL == "" {
		return nil, nil
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", baseURL)

	resp, err := httpclient.GlobalClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body) // drain to allow connection reuse
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if int64(len(body)) > MaxResponseBodySize {
		body = body[:MaxResponseBodySize]
	}

	if len(body) == 0 {
		return nil, nil
	}

	contentType := resp.Header.Get("Content-Type")

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return &CapturedResource{
		URL:        rawURL,
		Body:       body,
		MimeType:   contentType,
		StatusCode: int64(resp.StatusCode),
		Timestamp:  time.Now(),
		Headers:    headers,
	}, nil
}

// AssetPath maps a URL and MIME type to a local filesystem path for the asset.
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
	case strings.Contains(mime, "text/html"):
		return ".html"
	case strings.Contains(mime, "text/css"):
		return ".css"
	case strings.Contains(mime, "javascript"):
		return ".js"
	case strings.Contains(mime, "image/png"):
		return ".png"
	case strings.Contains(mime, "image/jpeg"):
		return ".jpg"
	case strings.Contains(mime, "image/gif"):
		return ".gif"
	case strings.Contains(mime, "image/svg"):
		return ".svg"
	case strings.Contains(mime, "image/webp"):
		return ".webp"
	case strings.Contains(mime, "image/x-icon"):
		return ".ico"
	case strings.Contains(mime, "font/woff2"):
		return ".woff2"
	case strings.Contains(mime, "font/woff"):
		return ".woff"
	case strings.Contains(mime, "font/ttf"):
		return ".ttf"
	case strings.Contains(mime, "font/eot"):
		return ".eot"
	case strings.Contains(mime, "application/json"):
		return ".json"
	case strings.Contains(mime, "application/pdf"):
		return ".pdf"
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

// HasURL reports whether a resource was captured for rawURL.
func (i *Interceptor) HasURL(rawURL string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	_, ok := i.resources[rawURL]
	return ok
}

// GetMissingResources returns seen URLs that have no captured resource.
func (i *Interceptor) GetMissingResources() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var missing []string
	for _, urlStr := range i.seen.Keys() {
		if _, ok := i.resources[urlStr]; !ok {
			missing = append(missing, urlStr)
		}
	}
	return missing
}

// GetAllSeenURLs returns all URLs seen during the session.
func (i *Interceptor) GetAllSeenURLs() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.seen.Keys()
}

// Close cancels in-flight fetches and waits for them to finish.
func (i *Interceptor) Close() {
	i.mu.Lock()
	if i.fetchCancel != nil {
		i.fetchCancel()
	}
	i.mu.Unlock()
	i.fetchWg.Wait()
}
