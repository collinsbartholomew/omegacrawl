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

type ClientPool struct {
	client    *stdhttp.Client
	transport *stdhttp.Transport
}

type ClientConfig struct {
	MaxIdleConns           int
	MaxIdleConnsPerHost    int
	MaxConnsPerHost        int
	IdleConnTimeout        time.Duration
	ConnectTimeout         time.Duration
	TLSHandshakeTimeout    time.Duration
	ResponseHeaderTimeout  time.Duration
	ExpectContinueTimeout  time.Duration
	DisableCompression     bool
}

func DefaultConfig() *ClientConfig {
	n := runtime.GOMAXPROCS(0)
	if n < 4 {
		n = 4
	}
	return &ClientConfig{
		MaxIdleConns:           n * 100,
		MaxIdleConnsPerHost:    n * 10,
		MaxConnsPerHost:        n * 10,
		IdleConnTimeout:        90 * time.Second,
		ConnectTimeout:         15 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		DisableCompression:     false,
	}
}

func GlobalClient() *stdhttp.Client {
	once.Do(func() {
		instance = NewClientPool(DefaultConfig())
	})
	return instance.client
}

func GlobalPool() *ClientPool {
	once.Do(func() {
		instance = NewClientPool(DefaultConfig())
	})
	return instance
}

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

	return &ClientPool{
		transport: transport,
		client: &stdhttp.Client{
			Transport: transport,
			Timeout:   0,
			CheckRedirect: func(req *stdhttp.Request, via []*stdhttp.Request) error {
				if len(via) >= 10 {
					return stdhttp.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

func (p *ClientPool) Client() *stdhttp.Client {
	return p.client
}

func (p *ClientPool) Transport() *stdhttp.Transport {
	return p.transport
}

func (p *ClientPool) CloseIdle() {
	p.transport.CloseIdleConnections()
}

func (p *ClientPool) CloneWithTLS(cfg *tls.Config) *stdhttp.Client {
	transport := p.transport.Clone()
	transport.TLSClientConfig = cfg
	return &stdhttp.Client{
		Transport: transport,
		Timeout:   p.client.Timeout,
	}
}
