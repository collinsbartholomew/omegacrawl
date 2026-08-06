package errors

import (
	"context"
	"errors"
)

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
