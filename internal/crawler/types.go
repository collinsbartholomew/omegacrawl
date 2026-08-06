package crawler

import "time"

type JSError struct {
	URL     string `json:"url"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

type WSMsg struct {
	URL       string    `json:"url"`
	Direction string    `json:"direction"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Opcode    float64   `json:"opcode"`
	IsBinary  bool      `json:"is_binary,omitempty"`
}

type CapturedAPIResponse struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	StatusCode  int               `json:"status_code"`
	Body        []byte            `json:"body"`
	Headers     map[string]string `json:"headers"`
	RequestBody []byte            `json:"request_body,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Size        int               `json:"size"`
	GraphQLOp   string            `json:"graphql_op,omitempty"`
}

const maxContentHashes = 1000000

const maxJSErrors = 10000

const maxWSMessages = 5000

const maxWSFrameSize = 10 * 1024 * 1024 // 10MB max per WS frame

const maxAPICaptures = 2000

const maxQueueSize = 100000

const drainTimeout = 30 * time.Second

const maxCookiesPerDomain = 50
