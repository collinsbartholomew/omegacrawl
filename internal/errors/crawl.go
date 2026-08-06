package errors

import (
	"errors"
	"fmt"
	"time"
)

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
