package network

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
)

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

	if ev.Request.Method != "GET" {
		// Post data is no longer directly available in Request; fetch it via CDP
		i.pendingRequests[ev.RequestID] = &APIRequest{
			URL:    ev.Request.URL,
			Method: ev.Request.Method,
			Body:   nil, // Will be populated in onLoadingFinished via GetRequestPostData
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

	// Fetch request post data if available (for non-GET requests)
	var reqBody []byte
	if p.request != nil && p.request.Method != "GET" {
		if postData, err := network.GetRequestPostData(p.requestID).Do(ctx); err == nil {
			reqBody = []byte(postData)
		}
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
			URL:         p.url,
			Body:        body,
			MimeType:    p.mimeType,
			StatusCode:  int(p.status),
			Headers:     p.headers,
			Method:      p.method,
			Timestamp:   time.Now(),
			Size:        len(body),
			Request:     p.request,
			RequestBody: reqBody,
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
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard interceptor response body", zap.Error(copyErr), zap.String("url", rawURL))
		}
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

// Reset clears the interceptor state for reuse.
func (i *Interceptor) Reset() {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Clear resources but keep the maps (to avoid reallocation)
	for k := range i.resources {
		delete(i.resources, k)
	}
	for k := range i.apiResponses {
		i.apiResponses[k] = APIResponse{}
	}
	i.apiResponses = i.apiResponses[:0]
	i.seen.Clear()
	for k := range i.pending {
		delete(i.pending, k)
	}
	for k := range i.pendingMethods {
		delete(i.pendingMethods, k)
	}
	for k := range i.pendingRequests {
		delete(i.pendingRequests, k)
	}
	i.baseURL = ""
	if i.fetchCancel != nil {
		i.fetchCancel()
		i.fetchCancel = nil
	}
	i.fetchCtx = nil
}

// InterceptorPool manages a pool of Interceptor instances for reuse across pages.
type InterceptorPool struct {
	mu          sync.Mutex
	available   []*Interceptor
	inUse       map[*Interceptor]bool
	maxSize     int
	workerCount int
	factory     func() *Interceptor
}

// NewInterceptorPool creates a new Interceptor pool.
func NewInterceptorPool(maxSize, workerCount int) *InterceptorPool {
	if maxSize < 1 {
		maxSize = 4
	}
	if workerCount < 1 {
		workerCount = 10
	}

	p := &InterceptorPool{
		available:   make([]*Interceptor, 0, maxSize),
		inUse:       make(map[*Interceptor]bool),
		maxSize:     maxSize,
		workerCount: workerCount,
	}

	// Pre-populate with one interceptor
	p.factory = func() *Interceptor {
		return NewInterceptorWithWorkers(workerCount)
	}
	p.available = append(p.available, p.factory())

	return p
}

// Acquire gets an Interceptor from the pool, blocking until one is available or ctx is cancelled.
func (p *InterceptorPool) Acquire(ctx context.Context) (*Interceptor, error) {
	// Fast path: try to acquire without blocking
	p.mu.Lock()
	if len(p.available) > 0 {
		// Reuse existing interceptor - reset its state
		i := p.available[len(p.available)-1]
		p.available = p.available[:len(p.available)-1]
		p.inUse[i] = true
		p.mu.Unlock()
		i.Reset()
		return i, nil
	}

	// Create new if under max size
	if len(p.inUse) < p.maxSize {
		i := p.factory()
		p.inUse[i] = true
		p.mu.Unlock()
		return i, nil
	}
	p.mu.Unlock()

	// Pool exhausted, wait for a release
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			p.mu.Lock()
			if len(p.available) > 0 {
				i := p.available[len(p.available)-1]
				p.available = p.available[:len(p.available)-1]
				p.inUse[i] = true
				p.mu.Unlock()
				i.Reset()
				return i, nil
			}
			p.mu.Unlock()
		}
	}
}

// Release returns an Interceptor to the pool.
func (p *InterceptorPool) Release(i *Interceptor) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.inUse[i]; !ok {
		// Not from our pool, just close it
		i.Close()
		return
	}

	delete(p.inUse, i)

	if len(p.available) < p.maxSize {
		i.Reset()
		p.available = append(p.available, i)
	} else {
		// Pool full, close it
		i.Close()
	}
}

// Close closes all interceptors in the pool.
func (p *InterceptorPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, i := range p.available {
		i.Close()
	}
	for i := range p.inUse {
		i.Close()
	}
	p.available = nil
	p.inUse = nil
}

// Stats returns pool statistics.
func (p *InterceptorPool) Stats() (available, inUse int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.available), len(p.inUse)
}
