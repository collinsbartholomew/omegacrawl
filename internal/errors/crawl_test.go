package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestSentinelFor(t *testing.T) {
	cases := map[Kind]error{
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
	for kind, want := range cases {
		if got := SentinelFor(kind); got != want {
			t.Errorf("SentinelFor(%v) = %v, want %v", kind, got, want)
		}
	}
	if got := SentinelFor(Kind(999)); got != ErrUnknown {
		t.Errorf("SentinelFor(unknown) = %v, want ErrUnknown", got)
	}
}

func TestErrorsIsSentinel(t *testing.T) {
	ce := New(KindTimeout, "request timed out")
	if !errors.Is(ce, ErrTimeout) {
		t.Errorf("expected errors.Is(ce, ErrTimeout) to be true")
	}

	wrapped := fmt.Errorf("outer: %w", ce)
	if !errors.Is(wrapped, ErrTimeout) {
		t.Errorf("expected errors.Is on wrapped error to find ErrTimeout")
	}

	if errors.Is(ce, ErrBlocked) {
		t.Errorf("expected errors.Is(ce, ErrBlocked) to be false")
	}

	wrappedCE := Wrap(KindRateLimit, "rate limited", fmt.Errorf("429 too many requests"))
	if !errors.Is(wrappedCE, ErrRateLimit) {
		t.Errorf("expected errors.Is(wrappedCE, ErrRateLimit) to be true")
	}
}

func TestClassifyProducesMatchingSentinel(t *testing.T) {
	err := fmt.Errorf("no such host: example.com")
	ce := Classify(err)
	if ce == nil {
		t.Fatal("expected Classify to return an error")
	}
	if !errors.Is(ce, ErrDNS) {
		t.Errorf("expected Classify DNS error to match ErrDNS, got kind %v", ce.Kind)
	}
}

func TestClassifyWrappedCrawlError(t *testing.T) {
	inner := New(KindTimeout, "timed out")
	wrapped := fmt.Errorf("outer: %w", inner)
	ce := Classify(wrapped)
	if ce == nil {
		t.Fatal("expected Classify to unwrap CrawlError")
	}
	if ce.Kind != KindTimeout {
		t.Errorf("expected KindTimeout, got %v", ce.Kind)
	}
}

func TestClassifyContextCancelled(t *testing.T) {
	ce := Classify(context.Canceled)
	if ce == nil {
		t.Fatal("expected Classify to classify context.Canceled")
	}
	if ce.Kind != KindCancelled {
		t.Errorf("expected KindCancelled, got %v", ce.Kind)
	}
	if !errors.Is(ce, ErrCancelled) {
		t.Errorf("expected errors.Is(ce, ErrCancelled) to be true")
	}
}

func TestClassifyStringCancellation(t *testing.T) {
	ce := Classify(fmt.Errorf("operation canceled by user"))
	if ce == nil {
		t.Fatal("expected Classify to classify cancellation string")
	}
	if ce.Kind != KindCancelled {
		t.Errorf("expected KindCancelled, got %v", ce.Kind)
	}
}

func TestClassifyReturnsNilForNil(t *testing.T) {
	if ce := Classify(nil); ce != nil {
		t.Errorf("expected nil for nil input, got %+v", ce)
	}
}

func TestIsWrappedSentinel(t *testing.T) {
	ce := Wrap(KindNetwork, "network error", ErrTimeout)
	if !errors.Is(ce, ErrTimeout) {
		t.Errorf("expected errors.Is(ce, ErrTimeout) for directly-wrapped sentinel")
	}
	if !errors.Is(ce, ErrNetwork) {
		t.Errorf("expected errors.Is(ce, ErrNetwork) for the kind sentinel")
	}
}

func TestIsNilSafe(t *testing.T) {
	var ce *CrawlError
	if errors.Is(ce, ErrTimeout) {
		t.Errorf("expected errors.Is(nil, ErrTimeout) to be false")
	}
	if errors.Is(Wrap(KindHTTP, "x", nil), nil) {
		t.Errorf("expected errors.Is(err, nil) to be false")
	}
}
