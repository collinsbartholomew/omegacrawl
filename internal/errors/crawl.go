package errors

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors allow callers to match against a crawl failure category with
// errors.Is regardless of how the error was wrapped. Each sentinel corresponds
// to one Kind.
var (
	// ErrTimeout indicates a request timeout.
	ErrTimeout = errors.New("crawl: timeout")
	// ErrNetwork indicates a network-level failure.
	ErrNetwork = errors.New("crawl: network")
	// ErrDNS indicates a DNS resolution failure.
	ErrDNS = errors.New("crawl: dns")
	// ErrTLS indicates a TLS or certificate failure.
	ErrTLS = errors.New("crawl: tls")
	// ErrHTTP indicates an HTTP-level error.
	ErrHTTP = errors.New("crawl: http")
	// ErrRateLimit indicates the server rate-limited the request.
	ErrRateLimit = errors.New("crawl: rate limited")
	// ErrBlocked indicates the request was blocked.
	ErrBlocked = errors.New("crawl: blocked")
	// ErrAuth indicates an authentication failure.
	ErrAuth = errors.New("crawl: auth")
	// ErrParse indicates a parsing failure.
	ErrParse = errors.New("crawl: parse")
	// ErrResource indicates a resource-level failure.
	ErrResource = errors.New("crawl: resource")
	// ErrBrowser indicates a browser automation failure.
	ErrBrowser = errors.New("crawl: browser")
	// ErrOOM indicates the process ran out of memory.
	ErrOOM = errors.New("crawl: out of memory")
	// ErrCancelled indicates the operation was cancelled.
	ErrCancelled = errors.New("crawl: cancelled")
	// ErrUnknown represents an unclassified error.
	ErrUnknown = errors.New("crawl: unknown")
)

// kindSentinel maps each Kind to its canonical sentinel error.
var kindSentinel = map[Kind]error{
	KindTimeout:   ErrTimeout,
	KindNetwork:   ErrNetwork,
	KindDNS:       ErrDNS,
	KindTLS:       ErrTLS,
	KindHTTP:      ErrHTTP,
	KindRateLimit: ErrRateLimit,
	KindBlocked:   ErrBlocked,
	KindAuth:      ErrAuth,
	KindParse:     ErrParse,
	KindResource:  ErrResource,
	KindBrowser:   ErrBrowser,
	KindOOM:       ErrOOM,
	KindCancelled: ErrCancelled,
	KindUnknown:   ErrUnknown,
}

// SentinelFor returns the sentinel error associated with the given kind.
func SentinelFor(kind Kind) error {
	if err, ok := kindSentinel[kind]; ok {
		return err
	}
	return ErrUnknown
}

// Kind identifies the category of a crawl error.
type Kind int

const (
	// KindUnknown represents an unclassified error.
	KindUnknown Kind = iota
	// KindTimeout indicates a request timeout.
	KindTimeout
	// KindNetwork indicates a network-level failure.
	KindNetwork
	// KindDNS indicates a DNS resolution failure.
	KindDNS
	// KindTLS indicates a TLS or certificate failure.
	KindTLS
	// KindHTTP indicates an HTTP-level error.
	KindHTTP
	// KindRateLimit indicates the server rate-limited the request.
	KindRateLimit
	// KindBlocked indicates the request was blocked.
	KindBlocked
	// KindAuth indicates an authentication failure.
	KindAuth
	// KindParse indicates a parsing failure.
	KindParse
	// KindResource indicates a resource-level failure.
	KindResource
	// KindBrowser indicates a browser automation failure.
	KindBrowser
	// KindOOM indicates the process ran out of memory.
	KindOOM
	// KindCancelled indicates the operation was cancelled.
	KindCancelled
)

// String returns the string representation of the Kind.
func (k Kind) String() string {
	switch k {
	case KindTimeout:
		return "timeout"
	case KindNetwork:
		return "network"
	case KindDNS:
		return "dns"
	case KindTLS:
		return "tls"
	case KindHTTP:
		return "http"
	case KindRateLimit:
		return "rate_limit"
	case KindBlocked:
		return "blocked"
	case KindAuth:
		return "auth"
	case KindParse:
		return "parse"
	case KindResource:
		return "resource"
	case KindBrowser:
		return "browser"
	case KindOOM:
		return "oom"
	case KindCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// CrawlError is a structured error carrying contextual metadata about a crawl failure.
type CrawlError struct {
	Kind       Kind
	Message    string
	Wrapped    error
	URL        string
	Host       string
	Retryable  bool
	StatusCode int
	Timestamp  time.Time
}

// Error implements the error interface.
func (e *CrawlError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Kind, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

// Unwrap returns the wrapped underlying error, if any.
func (e *CrawlError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Wrapped
}

// Is reports whether the error matches the target, matching sentinel errors by
// kind so errors.Is(err, errors.ErrTimeout) works on wrapped CrawlErrors.
func (e *CrawlError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	if target == e {
		return true
	}
	if target == SentinelFor(e.Kind) {
		return true
	}
	// A CrawlError may wrap a sentinel directly; surface it so errors.Is
	// succeeds against the underlying sentinel too.
	if e.Wrapped != nil && (target == e.Wrapped || errors.Is(e.Wrapped, target)) {
		return true
	}
	return false
}

// New creates a new CrawlError with the given kind and message.
func New(kind Kind, msg string) *CrawlError {
	return &CrawlError{
		Kind:      kind,
		Message:   msg,
		Retryable: IsRetryable(kind),
		Timestamp: time.Now(),
	}
}

// Wrap creates a new CrawlError wrapping the given underlying error.
func Wrap(kind Kind, msg string, err error) *CrawlError {
	return &CrawlError{
		Kind:      kind,
		Message:   msg,
		Wrapped:   err,
		Retryable: IsRetryable(kind),
		Timestamp: time.Now(),
	}
}

// IsRetryable reports whether errors of the given kind can be retried.
func IsRetryable(kind Kind) bool {
	switch kind {
	case KindTimeout, KindNetwork, KindRateLimit, KindBrowser, KindResource:
		return true
	case KindDNS, KindTLS, KindHTTP:
		return false
	case KindBlocked, KindAuth, KindParse, KindOOM, KindCancelled:
		return false
	default:
		return false
	}
}

// WithURL attaches the given URL to the error and returns the error.
func (e *CrawlError) WithURL(url string) *CrawlError {
	e.URL = url
	return e
}

// WithHost attaches the given host to the error and returns the error.
func (e *CrawlError) WithHost(host string) *CrawlError {
	e.Host = host
	return e
}

// WithStatusCode attaches the given HTTP status code to the error and returns the error.
func (e *CrawlError) WithStatusCode(code int) *CrawlError {
	e.StatusCode = code
	return e
}

// Classify inspects an arbitrary error and returns a categorized CrawlError.
func Classify(err error) *CrawlError {
	if err == nil {
		return nil
	}

	// Handle wrapped CrawlErrors (e.g. "%w" chains) via errors.As, not a
	// direct type assertion which misses wrapped instances.
	var ce *CrawlError
	if errors.As(err, &ce) {
		return ce
	}

	switch e := err.(type) {
	case interface{ Timeout() bool }:
		if e.Timeout() {
			return New(KindTimeout, "request timed out").WithStatusCode(408)
		}
	}

	if errors.Is(err, context.Canceled) {
		return New(KindCancelled, err.Error())
	}

	errStr := err.Error()

	if contains(errStr, "no such host") || contains(errStr, "DNS") || contains(errStr, "dns") {
		return New(KindDNS, errStr)
	}
	if contains(errStr, "tls") || contains(errStr, "certificate") || contains(errStr, "handshake") {
		return New(KindTLS, errStr)
	}
	if contains(errStr, "timeout") || contains(errStr, "deadline") {
		return New(KindTimeout, errStr)
	}
	if contains(errStr, "429") || contains(errStr, "rate limit") || contains(errStr, "too many") {
		return New(KindRateLimit, errStr).WithStatusCode(429)
	}
	if contains(errStr, "403") || contains(errStr, "blocked") || contains(errStr, "captcha") {
		return New(KindBlocked, errStr).WithStatusCode(403)
	}
	if contains(errStr, "401") || contains(errStr, "auth") || contains(errStr, "login") {
		return New(KindAuth, errStr).WithStatusCode(401)
	}
	if contains(errStr, "connection refused") || contains(errStr, "connection reset") || contains(errStr, "broken pipe") {
		return New(KindNetwork, errStr)
	}
	if contains(errStr, "no memory") || contains(errStr, "cannot allocate") {
		return New(KindOOM, errStr)
	}
	if contains(errStr, "canceled") || contains(errStr, "cancelled") {
		return New(KindCancelled, errStr)
	}

	return New(KindUnknown, errStr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
