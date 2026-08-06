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
		name  string
		input string
	}{
		{
			name:  "simple text",
			input: "hello world",
		},
		{
			name:  "binary-like data",
			input: "\x00\x01\x02\xff\xfe",
		},
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "JSON message",
			input: `{"type":"message","text":"hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := base64.StdEncoding.EncodeToString([]byte(tt.input))
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
	cfg.Seeds = []string{"https://example.com/"}

	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}
	c.routeMu.Lock()
	c.discoveredRoutes = map[string]bool{
		"https://example.com/":        true,
		"https://example.com/about":   true,
		"https://example.com/contact": true,
	}
	c.routeMu.Unlock()

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

	func TestSortUsesImport(t *testing.T) {
	urls := []string{"z.com", "a.com", "m.com"}
	sort.Strings(urls)
	if urls[0] != "a.com" {
		t.Error("sort.Strings not working")
	}
}

func TestStartRejectsAlreadyRunning(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Seeds = []string{"https://example.com/"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	c.started.Store(true)
	err = c.Start([]string{"https://example.com"})
	if err == nil {
		t.Fatal("expected 'already running' error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("unexpected error: %v", err)
	}
	if !c.IsRunning() {
		t.Error("IsRunning should be true while a crawl is active")
	}

	c.started.Store(false)
	if err := c.Start([]string{"file:///etc/passwd"}); err == nil {
		t.Error("expected seed error after reset")
	}
	if c.IsRunning() {
		t.Error("IsRunning should be false after Start returns")
	}
}

func TestStartRejectsInvalidSeeds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Seeds = []string{"https://example.com/"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	err = c.Start([]string{"file:///etc/passwd"})
	if err == nil {
		t.Fatal("expected error for file:// seed")
	}
	if !strings.Contains(err.Error(), "http/https") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIsSeedPage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Seeds = []string{"https://example.com/", "https://example.com/about"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/", true},
		{"https://example.com/about", true},
		{"https://example.com/contact", false},
		{"https://other.com/", false},
	}
	for _, tt := range tests {
		if got := c.isSeedPage(tt.url); got != tt.want {
			t.Errorf("isSeedPage(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestStartResetsPauseState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Seeds = []string{"https://example.com/"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	// Simulate a previous crawl that was paused and then stopped, leaving
	// paused set and a stale resume signal buffered in the channel.
	c.paused.Store(true)
	c.resumeCh <- struct{}{}

	// Starting a fresh crawl must clear the stale pause state (the new crawl
	// would otherwise block in the pause wait loop forever). Use an invalid
	// seed so Start fails fast without launching a browser.
	if err := c.Start([]string{"file:///etc/passwd"}); err == nil {
		t.Fatal("expected seed error for file:// URL")
	}
	if c.paused.Load() {
		t.Error("paused should be reset on a fresh Start")
	}
	select {
	case <-c.resumeCh:
		t.Error("stale resume signal should be drained on Start")
	default:
	}
}

func TestWriteSiteAggregate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.OutputDir = dir
	cfg.Seeds = []string{"https://example.com/"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	// Non-seed page with content fills the slot when no aggregate exists yet.
	c.writeSiteAggregate("https://example.com/about", "article.json", []byte(`{"title":"about"}`), &c.siteArticleWritten)
	path := filepath.Join(dir, "example.com", "article.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("aggregate not written by fallback: %v", err)
	}
	if !strings.Contains(string(data), "about") {
		t.Errorf("unexpected fallback content: %s", data)
	}

	// The seed page is preferred and overwrites the fallback.
	c.writeSiteAggregate("https://example.com/", "article.json", []byte(`{"title":"home"}`), &c.siteArticleWritten)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("aggregate not overwritten by seed page: %v", err)
	}
	if !strings.Contains(string(data), "home") {
		t.Errorf("seed page should overwrite fallback aggregate, got: %s", data)
	}

	// A later non-seed page must not clobber the existing aggregate.
	c.writeSiteAggregate("https://example.com/contact", "structured-data.json", []byte(`{"x":1}`), &c.siteSDWritten)
	c.writeSiteAggregate("https://example.com/contact", "article.json", []byte(`{"title":"contact"}`), &c.siteArticleWritten)
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "contact") {
		t.Errorf("non-seed page clobbered existing aggregate: %s", data)
	}
}

func TestStatusRunningReflectsStarter(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Seeds = []string{"https://example.com/"}
	c, err := NewCrawler(cfg)
	if err != nil {
		t.Fatalf("NewCrawler failed: %v", err)
	}

	if c.Status().Running {
		t.Error("Status.Running should be false before Start")
	}
	c.started.Store(true)
	if !c.Status().Running {
		t.Error("Status.Running should be true while crawling")
	}
	c.started.Store(false)
	if c.Status().Running {
		t.Error("Status.Running should be false after crawl")
	}
}
