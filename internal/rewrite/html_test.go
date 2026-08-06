package rewrite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriter_RewriteHTML(t *testing.T) {
	r := NewRewriter()

	htmlDir := "/output/example.com/page"
	cssPath := filepath.Join(htmlDir, "..", "assets/style.css")
	jsPath := filepath.Join(htmlDir, "../assets/script.js")

	r.AddMapping("https://example.com/assets/style.css", cssPath)
	r.AddMapping("https://example.com/assets/script.js", jsPath)

	html := []byte(`<html><head><link href="https://example.com/assets/style.css"></head><body><script src="https://example.com/assets/script.js"></script></body></html>`)

	htmlPath := filepath.Join(htmlDir, "index.html")
	result := r.RewriteHTML(html, htmlPath)

	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}

	resultStr := string(result)
	if contains(resultStr, "https://example.com/assets/script.js") {
		t.Error("expected script URL to be rewritten")
	}

	if !contains(resultStr, "assets/script.js") {
		t.Error("expected relative path to script")
	}
}

func TestRewriter_RewriteCSS(t *testing.T) {
	r := NewRewriter()

	cssDir := "/output/example.com/assets"
	bgPath := filepath.Join(cssDir, "bg.png")

	r.AddMapping("https://example.com/assets/bg.png", bgPath)

	css := []byte(`body { background: url("https://example.com/assets/bg.png"); }`)

	cssPath := filepath.Join(cssDir, "style.css")
	result := r.RewriteCSS(css, cssPath)

	resultStr := string(result)
	if contains(resultStr, "https://example.com/assets/bg.png") {
		t.Error("expected CSS URL to be rewritten")
	}

	if !contains(resultStr, "bg.png") {
		t.Error("expected relative path in CSS")
	}
}

func TestRewriter_ExtractLinks(t *testing.T) {
	r := NewRewriter()

	html := []byte(`<html><body><a href="/page1">Link1</a><a href="https://example.com/page2">Link2</a><a href="javascript:void(0)">JS</a><a href="mailto:test@test.com">Email</a><a href="#section">Anchor</a></body></html>`)

	links := r.ExtractLinks("https://example.com/index.html", html)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d: %v", len(links), links)
	}
}

func TestRewriter_ProcessFiles(t *testing.T) {
	dir := t.TempDir()
	r := NewRewriter()

	r.AddMapping("https://example.com/style.css", filepath.Join(dir, "style.css"))

	html := []byte(`<html><link href="https://example.com/style.css"></html>`)
	htmlPath := filepath.Join(dir, "index.html")
	os.WriteFile(htmlPath, html, 0644)

	r.ProcessFiles(map[string]string{
		htmlPath: "html",
	})

	result, _ := os.ReadFile(htmlPath)
	resultStr := string(result)

	if contains(resultStr, "https://example.com/style.css") {
		t.Error("expected URL to be rewritten in file")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestInjectWSReplayScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "injects before </body>",
			input:    `<html><body><p>Hello</p></body></html>`,
			expected: `<script src="ws-replay.js"></script>`,
		},
		{
			name:     "injects before </html> when no </body>",
			input:    `<html><body><p>Hello</p></html>`,
			expected: `<script src="ws-replay.js"></script>`,
		},
		{
			name:     "no injection for non-HTML",
			input:    `<svg><text>Hello</text></svg>`,
			expected: `<svg><text>Hello</text></svg>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(injectWSReplayScript([]byte(tt.input)))
			if tt.name == "no injection for non-HTML" {
				if result != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, result)
				}
				return
			}
			if !contains(result, tt.expected) {
				t.Errorf("expected result to contain %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractFontURLs(t *testing.T) {
	r := NewRewriter()

	tests := []struct {
		name     string
		css      string
		expected []string
	}{
		{
			name: "single font-face",
			css: `@font-face {
				font-family: 'MyFont';
				src: url('https://example.com/fonts/myfont.woff2') format('woff2'),
				     url('https://example.com/fonts/myfont.woff') format('woff');
			}`,
			expected: []string{
				"https://example.com/fonts/myfont.woff2",
				"https://example.com/fonts/myfont.woff",
			},
		},
		{
			name: "multiple font-faces",
			css: `@font-face { font-family: 'A'; src: url('a.woff2') format('woff2'); }
				@font-face { font-family: 'B'; src: url('b.woff2') format('woff2'); }`,
			expected: []string{"a.woff2", "b.woff2"},
		},
		{
			name:     "no font-face",
			css:      `body { color: red; }`,
			expected: nil,
		},
		{
			name:     "empty CSS",
			css:      "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls := r.ExtractFontURLs([]byte(tt.css))
			if len(urls) != len(tt.expected) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.expected), len(urls), urls)
			}
			for i, u := range tt.expected {
				if urls[i] != u {
					t.Errorf("URL[%d]: expected %q, got %q", i, u, urls[i])
				}
			}
		})
	}
}

func TestExtractFontURLs_NoDoubleCounting(t *testing.T) {
	r := NewRewriter()
	css := `@font-face { font-family: 'A'; src: url('font.woff2') format('woff2'), url('font.woff2') format('woff'); }`
	urls := r.ExtractFontURLs([]byte(css))
	if len(urls) != 1 {
		t.Errorf("expected 1 unique URL, got %d: %v", len(urls), urls)
	}
}

func TestExtractAllCSSURLs(t *testing.T) {
	r := NewRewriter()

	tests := []struct {
		name     string
		css      string
		expected []string
	}{
		{
			name:     "background images",
			css:      `body { background: url("https://example.com/bg.png"); } .card { background-image: url('card.jpg'); }`,
			expected: []string{"https://example.com/bg.png", "card.jpg"},
		},
		{
			name:     "font-face and background",
			css:      `@font-face { src: url('font.woff2'); } body { background: url(bg.jpg); }`,
			expected: []string{"font.woff2", "bg.jpg"},
		},
		{
			name:     "data URIs excluded",
			css:      `body { background: url("data:image/png;base64,abc"); }`,
			expected: nil,
		},
		{
			name:     "escaped parentheses in url",
			css:      `body { background: url(icon\(1\).png); }`,
			expected: []string{`icon\(1\).png`},
		},
		{
			name:     "escaped parentheses in quoted url",
			css:      `body { background: url("icons/icon\(big\).png"); }`,
			expected: []string{`icons/icon\(big\).png`},
		},
		{
			name:     "no urls",
			css:      `body { color: red; }`,
			expected: nil,
		},
		{
			name:     "empty CSS",
			css:      "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls := r.ExtractAllCSSURLs([]byte(tt.css))
			if len(urls) != len(tt.expected) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.expected), len(urls), urls)
			}
			for i, u := range tt.expected {
				if urls[i] != u {
					t.Errorf("URL[%d]: expected %q, got %q", i, u, urls[i])
				}
			}
		})
	}
}
