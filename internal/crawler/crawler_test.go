package crawler

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/user/clone/internal/config"
)

func TestExtractGraphQLOp(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "basic operation",
			body:     `{"operationName":"getUsers","query":"query getUsers { users { id } }"}`,
			expected: "getUsers",
		},
		{
			name:     "mutation operation",
			body:     `{"operationName":"deleteUser","query":"mutation deleteUser($id:ID!) { deleteUser(id:$id) { success } }"}`,
			expected: "deleteUser",
		},
		{
			name:     "empty operation",
			body:     `{"query":"query { users { id } }"}`,
			expected: "",
		},
		{
			name:     "empty body",
			body:     "",
			expected: "",
		},
		{
			name:     "invalid JSON",
			body:     `not json`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGraphQLOp([]byte(tt.body))
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBase64EncodePayload(t *testing.T) {
	tests := []struct {
		name     string
		input    string
	}{
		{
			name:     "simple text",
			input:    "hello world",
		},
		{
			name:     "binary-like data",
			input:    "\x00\x01\x02\xff\xfe",
		},
		{
			name:     "empty",
			input:    "",
		},
		{
			name:     "JSON message",
			input:    `{"type":"message","text":"hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64EncodePayload(tt.input)
			decoded, err := base64.StdEncoding.DecodeString(result)
			if err != nil {
				t.Fatalf("base64 decode failed: %v", err)
			}
			if string(decoded) != tt.input {
				t.Errorf("roundtrip failed: expected %q, got %q", tt.input, string(decoded))
			}
		})
	}
}

func TestWriteSitemap(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.OutputDir = dir

	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}
	c.visitedURLs = map[string]*URLVisitInfo{
		"https://example.com/":      {URL: "https://example.com/"},
		"https://example.com/about": {URL: "https://example.com/about"},
		"https://example.com/contact": {URL: "https://example.com/contact"},
	}

	c.writeSitemap()

	data, err := os.ReadFile(filepath.Join(dir, "sitemap.xml"))
	if err != nil {
		t.Fatalf("failed to read sitemap: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(content, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`) {
		t.Error("missing urlset tag")
	}
	if !strings.Contains(content, "https://example.com/") {
		t.Error("missing example.com URL")
	}
	if !strings.Contains(content, "https://example.com/about") {
		t.Error("missing about URL")
	}
	if !strings.Contains(content, "https://example.com/contact") {
		t.Error("missing contact URL")
	}
	if !strings.Contains(content, "</urlset>") {
		t.Error("missing closing urlset")
	}
}

func TestCapturedAPIResponseJSON(t *testing.T) {
	resp := CapturedAPIResponse{
		URL:         "https://api.example.com/data",
		Method:      "POST",
		StatusCode:  200,
		Body:        []byte(`{"result":"ok"}`),
		Headers:     map[string]string{"Content-Type": "application/json"},
		RequestBody: []byte(`{"query":"test"}`),
		Timestamp:   time.Now(),
		Size:        15,
		GraphQLOp:   "getData",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded CapturedAPIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.URL != resp.URL {
		t.Errorf("URL mismatch: %q vs %q", decoded.URL, resp.URL)
	}
	if decoded.GraphQLOp != "getData" {
		t.Errorf("GraphQLOp mismatch: %q vs %q", decoded.GraphQLOp, "getData")
	}
	if string(decoded.Body) != string(resp.Body) {
		t.Errorf("Body mismatch: %q vs %q", string(decoded.Body), string(resp.Body))
	}
}

func TestWSMsgJSON(t *testing.T) {
	msg := WSMsg{
		URL:       "wss://example.com/ws",
		Direction: "receive",
		Data:      "hello",
		Timestamp: time.Now(),
		Opcode:    2,
		IsBinary:  true,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded WSMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.URL != msg.URL {
		t.Errorf("URL mismatch: %q vs %q", decoded.URL, msg.URL)
	}
	if decoded.IsBinary != true {
		t.Error("IsBinary should be true")
	}
	if decoded.Opcode != 2 {
		t.Errorf("Opcode mismatch: %f vs %f", decoded.Opcode, 2.0)
	}
}

func TestJsonHeadrs(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected string
	}{
		{
			name:     "empty",
			input:    map[string]string{},
			expected: "{}",
		},
		{
			name:     "single header",
			input:    map[string]string{"Content-Type": "application/json"},
			expected: `{"Content-Type":"application/json"}`,
		},
		{
			name:     "multiple headers",
			input:    map[string]string{"Content-Type": "text/html", "X-Custom": "value"},
			expected: `{"Content-Type":"text/html","X-Custom":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jsonHeadrs(tt.input)
			if result == tt.expected {
				return
			}
			var a, b map[string]string
			json.Unmarshal([]byte(result), &a)
			json.Unmarshal([]byte(tt.expected), &b)
			if len(a) != len(b) {
				t.Errorf("header count mismatch: %d vs %d", len(a), len(b))
			}
			for k, v := range a {
				if b[k] != v {
					t.Errorf("header %q mismatch: %q vs %q", k, v, b[k])
				}
			}
		})
	}
}

func TestSortUsesImport(t *testing.T) {
	urls := []string{"z.com", "a.com", "m.com"}
	sort.Strings(urls)
	if urls[0] != "a.com" {
		t.Error("sort.Strings not working")
	}
}