package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAssetURLs(t *testing.T) {
	html := `<html><head>
<meta property="og:image" content="https://example.com/og.png">
<link rel="stylesheet" href="https://example.com/assets/style.css">
</head><body>
<img src="https://example.com/images/logo.png">
<img srcset="https://example.com/a.jpg 1x, https://example.com/b.jpg 2x">
<script src="https://example.com/app.js"></script>
<a href="https://example.com/other-page">page</a>
<a href="https://example.com/file.pdf">pdf</a>
<div style="background: url(https://example.com/bg.png)"></div>
</body></html>`

	urls := extractAssetURLs([]byte(html), "https://example.com/")
	want := map[string]bool{
		"https://example.com/og.png":           true,
		"https://example.com/assets/style.css": true,
		"https://example.com/images/logo.png":  true,
		"https://example.com/a.jpg":            true,
		"https://example.com/b.jpg":            true,
		"https://example.com/app.js":           true,
		"https://example.com/file.pdf":         true,
		"https://example.com/bg.png":           true,
	}
	for u := range want {
		if !urls[u] {
			t.Errorf("expected asset URL %s to be extracted", u)
		}
	}
	for u := range urls {
		if !want[u] {
			t.Errorf("unexpected asset URL %s", u)
		}
	}
}

func TestExtractCSSURLsEscapes(t *testing.T) {
	css := `.a { background: url(https://example.com/fonts/foo\(bar\).woff2); }
.b { background: url("https://example.com/spaced%20file.png"); }`
	urls := extractCSSURLs(css)
	joined := strings.Join(urls, "|")
	for _, want := range []string{
		"https://example.com/fonts/foo(bar).woff2",
		"https://example.com/spaced%20file.png",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in extracted URLs %q", want, urls)
		}
	}
}

func TestRewriteRepairedURLs(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "site", "home", "index.html")
	mapping := map[string]string{
		"https://example.com/assets/img.png": filepath.Join(dir, "site", "assets", "img.png"),
		"https://example.com/assets/style.css": filepath.Join(dir, "site", "assets", "style.css"),
	}
	html := []byte(`<link href="https://example.com/assets/style.css"><img src="https://example.com/assets/img.png">`)
	rewritten := rewriteRepairedURLs(html, page, mapping)

	for _, rel := range []string{"../assets/style.css", "../assets/img.png"} {
		if !strings.Contains(string(rewritten), rel) {
			t.Errorf("expected rewritten HTML to contain %q, got %q", rel, rewritten)
		}
	}
	if strings.Contains(string(rewritten), "https://example.com/assets/") {
		t.Errorf("absolute URLs should be replaced, got %q", rewritten)
	}
}

func TestCSSEscapeURL(t *testing.T) {
	got := cssEscapeURL(`https://example.com/a(b),c'`)
	want := `https://example.com/a\(b\)\,c\'`
	if got != want {
		t.Errorf("cssEscapeURL = %q, want %q", got, want)
	}
}

// TestExtractAssetURLsHomeTwo is an integration smoke check against a real
// clone when one exists on disk; it is skipped otherwise so CI without sample
// output still passes. It only verifies extraction runs without error and
// logs the result, since sample pages may already be fully localized (zero
// absolute URLs).
func TestExtractAssetURLsHomeTwo(t *testing.T) {
	path := filepath.Join("..", "..", "output", "farmex2", "farmex-webflow-template.webflow.io", "home-two", "index.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("sample output not present, skipping: %v", err)
	}
	urls := extractAssetURLs(data, "https://farmex-webflow-template.webflow.io/home-two/")
	t.Logf("sample page yielded %d asset URLs", len(urls))
}
