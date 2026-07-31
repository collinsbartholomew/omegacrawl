package crawler

import (
	"math"
	"math/rand"
	"time"
)

// RetryConfig controls exponential backoff with jitter for retried requests.
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	JitterFraction float64
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// GetBackoff returns the backoff duration to wait before the given retry attempt.
func (r *RetryConfig) GetBackoff(attempt int) time.Duration {
	backoff := float64(r.InitialBackoff) * math.Pow(r.Multiplier, float64(attempt))

	if backoff > float64(r.MaxBackoff) {
		backoff = float64(r.MaxBackoff)
	}

	jitter := backoff * r.JitterFraction * (rand.Float64()*2 - 1)
	backoff += jitter

	return time.Duration(backoff)
}

// RetryableError wraps an error that may be retried based on the HTTP status code.
type RetryableError struct {
	Err        error
	Retryable  bool
	StatusCode int
}

// Error returns the string representation of the wrapped error.
func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable reports whether the given HTTP status code indicates a retryable failure.
func IsRetryable(statusCode int) bool {
	switch {
	case statusCode == 429:
		return true
	case statusCode == 503:
		return true
	case statusCode == 504:
		return true
	case statusCode == 408:
		return true
	case statusCode >= 500:
		return true
	default:
		return false
	}
}

// IsRateLimited reports whether the given HTTP status code indicates rate limiting.
func IsRateLimited(statusCode int) bool {
	return statusCode == 429 || statusCode == 503
}

// ParseRetryAfter parses a Retry-After header value into a duration; it returns
// 0 if the value is empty or cannot be parsed.
func ParseRetryAfter(retryAfter string) time.Duration {
	if retryAfter == "" {
		return 0
	}

	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil {
		return seconds
	}

	if t, err := time.Parse(time.RFC1123, retryAfter); err == nil {
		return time.Until(t)
	}

	return 0
}
