package rewrite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDebugCSSFiles(t *testing.T) {
	r := NewRewriter()
	
	// Simulate adding a CSS file mapping
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	projectRoot := filepath.Dir(filepath.Dir(dir))
	cssPath := filepath.Join(projectRoot, "test-localized", "cdn.prod.website-files.com", "673ff5cf97fea2539486915c", "css", "farmex-webflow-template.shared.fa7d74cbd.min.css")
	
	t.Logf("CSS path: %s", cssPath)
	t.Logf("Exists: %v", fileExists(cssPath))
	
	if fileExists(cssPath) {
		r.AddMapping("https://cdn.prod.website-files.com/673ff5cf97fea2539486915c/css/farmex-webflow-template.shared.fa7d74cbd.min.css", cssPath)
		
		cssFiles := r.GetCSSFiles()
		t.Logf("CSS files in rewriter: %v", cssFiles)
		
		for path := range cssFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Logf("Error reading: %v", err)
				continue
			}
			fontURLs := r.ExtractFontURLs(data)
			cssURLs := r.ExtractAllCSSURLs(data)
			t.Logf("Font URLs: %d", len(fontURLs))
			t.Logf("CSS URLs: %d", len(cssURLs))
			for _, u := range cssURLs {
				if contains(u, "Hero") || contains(u, "BG") || contains(u, "Icon") {
					t.Logf("  ESCAPED: %s", u)
				}
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
