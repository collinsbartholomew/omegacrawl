package httpclient

import (
	"crypto/tls"
	"net"
	stdhttp "net/http"
	"runtime"
	"sync"
	"time"
)

var (
	instance *ClientPool
	once     sync.Once
)

// ClientPool manages a shared HTTP client and its underlying transport.
type ClientPool struct {
	client    *stdhttp.Client
	transport *stdhttp.Transport
}

// ClientConfig holds tuning parameters for the pooled HTTP client.
type ClientConfig struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	ConnectTimeout        time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration
	DisableCompression    bool
	Timeout               time.Duration
}

// DefaultConfig returns a ClientConfig with sensible defaults sized to the host CPU count.
func DefaultConfig() *ClientConfig {
	n := runtime.GOMAXPROCS(0)
	if n < 4 {
		n = 4
	}
	return &ClientConfig{
		MaxIdleConns:          n * 100,
		MaxIdleConnsPerHost:   n * 10,
		MaxConnsPerHost:       n * 10,
		IdleConnTimeout:       90 * time.Second,
		ConnectTimeout:        15 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
	}
}

// GlobalClient returns the process-wide shared HTTP client.
func GlobalClient() *stdhttp.Client {
	once.Do(func() {
		instance = NewClientPool(DefaultConfig())
	})
	return instance.client
}

// GlobalPool returns the process-wide shared ClientPool.
func GlobalPool() *ClientPool {
	once.Do(func() {
		instance = NewClientPool(DefaultConfig())
	})
	return instance
}

// NewClientPool builds a ClientPool from the given config, or defaults if cfg is nil.
func NewClientPool(cfg *ClientConfig) *ClientPool {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	transport := &stdhttp.Transport{
		DialContext: (&net.Dialer{
			Timeout:   cfg.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		ExpectContinueTimeout: cfg.ExpectContinueTimeout,
		DisableCompression:    cfg.DisableCompression,
		ForceAttemptHTTP2:     true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	clientTimeout := cfg.Timeout
	if clientTimeout <= 0 {
		clientTimeout = 120 * time.Second
	}

	return &ClientPool{
		transport: transport,
		client: &stdhttp.Client{
			Transport: transport,
			Timeout:   clientTimeout,
			CheckRedirect: func(req *stdhttp.Request, via []*stdhttp.Request) error {
				if len(via) >= 10 {
					return stdhttp.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// Client returns the pooled HTTP client.
func (p *ClientPool) Client() *stdhttp.Client {
	return p.client
}

// Transport returns the pooled HTTP transport.
func (p *ClientPool) Transport() *stdhttp.Transport {
	return p.transport
}

// CloseIdle closes any idle connections held by the pooled transport.
func (p *ClientPool) CloseIdle() {
	p.transport.CloseIdleConnections()
}

// CloneWithTLS returns a new HTTP client cloned from the pool's transport with the given TLS config.
func (p *ClientPool) CloneWithTLS(cfg *tls.Config) *stdhttp.Client {
	transport := p.transport.Clone()
	transport.TLSClientConfig = cfg
	return &stdhttp.Client{
		Transport: transport,
		Timeout:   p.client.Timeout,
	}
}
