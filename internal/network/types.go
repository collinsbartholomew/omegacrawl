package network

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
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
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Body        []byte            `json:"body"`
	MimeType    string            `json:"mime_type"`
	StatusCode  int               `json:"status_code"`
	Headers     map[string]string `json:"headers"`
	Timestamp   time.Time         `json:"timestamp"`
	Size        int               `json:"size"`
	Request     *APIRequest       `json:"request,omitempty"`
	RequestBody []byte            `json:"request_body,omitempty"`
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
