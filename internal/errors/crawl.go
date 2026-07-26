package errors

import (
	"fmt"
	"time"
)

type Kind int

const (
	KindUnknown    Kind = iota
	KindTimeout
	KindNetwork
	KindDNS
	KindTLS
	KindHTTP
	KindRateLimit
	KindBlocked
	KindAuth
	KindParse
	KindResource
	KindBrowser
	KindOOM
	KindCancelled
)

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

type CrawlError struct {
	Kind      Kind
	Message   string
	Wrapped   error
	URL       string
	Host      string
	Retryable bool
	StatusCode int
	Timestamp time.Time
}

func (e *CrawlError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Kind, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

func (e *CrawlError) Unwrap() error {
	return e.Wrapped
}

func New(kind Kind, msg string) *CrawlError {
	return &CrawlError{
		Kind:      kind,
		Message:   msg,
		Retryable: IsRetryable(kind),
		Timestamp: time.Now(),
	}
}

func Wrap(kind Kind, msg string, err error) *CrawlError {
	return &CrawlError{
		Kind:      kind,
		Message:   msg,
		Wrapped:   err,
		Retryable: IsRetryable(kind),
		Timestamp: time.Now(),
	}
}

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

func (e *CrawlError) WithURL(url string) *CrawlError {
	e.URL = url
	return e
}

func (e *CrawlError) WithHost(host string) *CrawlError {
	e.Host = host
	return e
}

func (e *CrawlError) WithStatusCode(code int) *CrawlError {
	e.StatusCode = code
	return e
}

func Classify(err error) *CrawlError {
	if err == nil {
		return nil
	}

	if ce, ok := err.(*CrawlError); ok {
		return ce
	}

	switch e := err.(type) {
	case interface{ Timeout() bool }:
		if e.Timeout() {
			return New(KindTimeout, "request timed out").WithStatusCode(408)
		}
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
