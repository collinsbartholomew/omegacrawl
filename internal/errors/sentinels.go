package errors

import "errors"

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
