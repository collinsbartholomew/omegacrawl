package network

import (
	"testing"
)

func TestIsAPIContentType(t *testing.T) {
	tests := []struct {
		url      string
		mime     string
		expected bool
	}{
		{"https://example.com/api/users", "text/html", true},
		{"https://example.com/graphql", "application/json", true},
		{"https://example.com/gql", "text/plain", true},
		{"https://example.com/rest/v1/users", "application/json", true},
		{"https://example.com/v1/users", "application/json", true},
		{"https://example.com/api", "text/xml", true},
		{"https://example.com/page", "text/html", false},
		{"https://example.com/style.css", "text/css", false},
		{"https://example.com/script.js", "text/javascript", false},
		{"https://example.com/rpc/method", "application/grpc", true},
		{"https://example.com/jsonrpc", "application/json", true},
	}
	for _, tt := range tests {
		result := isAPIContentType(tt.url, tt.mime)
		if result != tt.expected {
			t.Errorf("isAPIContentType(%q, %q) = %v; want %v", tt.url, tt.mime, result, tt.expected)
		}
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
		{"text/plain", false},
		{"application/xml", false},
	}
	for _, tt := range tests {
		result := isJSONContentType(tt.mime)
		if result != tt.expected {
			t.Errorf("isJSONContentType(%q) = %v; want %v", tt.mime, result, tt.expected)
		}
	}
}

func TestBaseMimeType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"text/html; charset=utf-8", "text/html"},
		{"text/html", "text/html"},
		{"application/json; charset=utf-8", "application/json"},
		{"application/json", "application/json"},
		{"", ""},
	}
	for _, tt := range tests {
		result := baseMimeType(tt.input)
		if result != tt.expected {
			t.Errorf("baseMimeType(%q) = %q; want %q", tt.input, result, tt.expected)
		}
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
		{"image/webp", ".webp"},
		{"font/woff", ".woff"},
		{"font/woff2", ".woff2"},
		{"font/ttf", ".ttf"},
		{"application/json", ".json"},
		{"application/octet-stream", ""},
		{"text/plain", ""},
	}
	for _, tt := range tests {
		result := extensionForMime(tt.mime)
		if result != tt.expected {
			t.Errorf("extensionForMime(%q) = %q; want %q", tt.mime, result, tt.expected)
		}
	}
}
