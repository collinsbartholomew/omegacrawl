package errors

import "time"

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
