package crawler

import (
	"math"
	"math/rand"
	"time"
)

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	JitterFraction float64
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

func (r *RetryConfig) GetBackoff(attempt int) time.Duration {
	backoff := float64(r.InitialBackoff) * math.Pow(r.Multiplier, float64(attempt))

	if backoff > float64(r.MaxBackoff) {
		backoff = float64(r.MaxBackoff)
	}

	jitter := backoff * r.JitterFraction * (rand.Float64()*2 - 1)
	backoff += jitter

	return time.Duration(backoff)
}

type RetryableError struct {
	Err         error
	Retryable   bool
	StatusCode  int
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

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

func IsRateLimited(statusCode int) bool {
	return statusCode == 429 || statusCode == 503
}

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
