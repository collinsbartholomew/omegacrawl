package network

import (
	"testing"
)

func TestIsAPIContentType(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mime     string
		expected bool
	}{
		{
			name:     "XML API response",
			url:      "https://example.com/api/data",
			mime:     "application/xml",
			expected: true,
		},
		{
			name:     "JSON API response",
			url:      "https://example.com/api/data",
			mime:     "application/json",
			expected: true,
		},
		{
			name:     "GraphQL endpoint",
			url:      "https://example.com/graphql",
			mime:     "text/html",
			expected: true,
		},
		{
			name:     "REST v1 endpoint",
			url:      "https://example.com/v1/users",
			mime:     "text/html",
			expected: true,
		},
		{
			name:     "regular HTML page",
			url:      "https://example.com/page",
			mime:     "text/html",
			expected: false,
		},
		{
			name:     "CSS file",
			url:      "https://example.com/style.css",
			mime:     "text/css",
			expected: false,
		},
		{
			name:     "protobuf content type",
			url:      "https://example.com/data",
			mime:     "application/protobuf",
			expected: true,
		},
		{
			name:     "gRPC",
			url:      "https://example.com/Service/Method",
			mime:     "application/grpc",
			expected: true,
		},
		{
			name:     "RPC endpoint",
			url:      "https://example.com/jsonrpc",
			mime:     "text/html",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAPIContentType(tt.url, tt.mime)
			if result != tt.expected {
				t.Errorf("isAPIContentType(%q, %q) = %v, want %v", tt.url, tt.mime, result, tt.expected)
			}
		})
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		mime     string
		expected bool
	}{
		{"application/json", true},
		{"text/json", true},
		{"application/vnd.api+json", true},
		{"application/problem+json", true},
		{"application/hal+json", true},
		{"application/ld+json", true},
		{"text/html", false},
		{"text/css", false},
		{"application/xml", false},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			result := isJSONContentType(tt.mime)
			if result != tt.expected {
				t.Errorf("isJSONContentType(%q) = %v, want %v", tt.mime, result, tt.expected)
			}
		})
	}
}

func TestBaseMimeType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"application/json", "application/json"},
		{"text/html; charset=utf-8", "text/html"},
		{"text/plain; boundary=something", "text/plain"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := baseMimeType(tt.input)
			if result != tt.expected {
				t.Errorf("baseMimeType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtensionForMime(t *testing.T) {
	tests := []struct {
		mime     string
		expected string
	}{
		{"text/html", ".html"},
		{"text/css", ".css"},
		{"text/javascript", ".js"},
		{"application/javascript", ".js"},
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/svg+xml", ".svg"},
		{"font/woff", ".woff"},
		{"font/woff2", ".woff2"},
		{"application/json", ".json"},
		{"text/plain", ""},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			result := extensionForMime(tt.mime)
			if result != tt.expected {
				t.Errorf("extensionForMime(%q) = %q, want %q", tt.mime, result, tt.expected)
			}
		})
	}
}