package rewrite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractAllCSSURLs_RealCSS(t *testing.T) {
	r := NewRewriter()

	// Get the project root
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	// Go up from internal/rewrite to project root
	projectRoot := filepath.Dir(filepath.Dir(dir))
	cssPath := filepath.Join(projectRoot, "test-raw", "cdn.prod.website-files.com", "673ff5cf97fea2539486915c", "css", "farmex-webflow-template.shared.fa7d74cbd.min.css")

	cssData, err := os.ReadFile(cssPath)
	if err != nil {
		t.Skip("test-raw CSS not available:", err)
	}

	cssURLs := r.ExtractAllCSSURLs(cssData)
	t.Logf("CSS URLs found: %d", len(cssURLs))

	// Check for the specific escaped-paren URLs
	escapedFound := 0
	for _, u := range cssURLs {
		if contains(u, "Hero%20") || contains(u, "BG%20") || contains(u, "Icon%20") {
			escapedFound++
			t.Logf("FOUND ESCAPED: %s", u)
		}
	}
	t.Logf("Escaped-paren URLs found: %d", escapedFound)

	// Also check font URLs
	fontURLs := r.ExtractFontURLs(cssData)
	t.Logf("Font URLs found: %d", len(fontURLs))
	for _, u := range fontURLs {
		t.Logf("  Font: %s", u)
	}
}
