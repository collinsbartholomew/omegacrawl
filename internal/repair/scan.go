package repair

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/clone/internal/storage"
)

// findPages returns the HTML page files in a clone directory. A page is an
// index.html file that is rooted directly under a host directory (at least
// one path segment below the output root) and whose content is HTML.
func findPages(root string) ([]string, error) {
	var pages []string
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return nil
		}
		segments := strings.Split(rel, string(filepath.Separator))
		if len(segments) < 2 {
			return nil
		}
		if fi.Name() != "index.html" {
			return nil
		}
		if isBinaryHTML(p) {
			return nil
		}
		pages = append(pages, p)
		return nil
	})
	return pages, err
}

// findStylesheets returns the stylesheet files in a clone directory. Repair
// rewrites standalone CSS files the same way as pages because the crawler's
// CSS rewriter cannot localize backslash-escaped url() references.
func findStylesheets(root string) ([]string, error) {
	var cssFiles []string
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".css") {
			return nil
		}
		cssFiles = append(cssFiles, p)
		return nil
	})
	return cssFiles, err
}

// isBinaryHTML reports whether the file at path contains binary (non-HTML)
// bytes, e.g. an image misnamed index.html under _unknown/.
func isBinaryHTML(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	head := strings.ToLower(string(buf))
	if strings.Contains(head, "<html") || strings.HasPrefix(head, "<!doctype") {
		return false
	}

	return strings.IndexByte(string(buf), 0) >= 0
}

// urlFromLocalPath reconstructs the https URL a local file was saved from,
// reversing storage.Filesystem.PathForURL. Spaces are re-encoded as %20 so
// the result matches the strings used inside the saved HTML.
func urlFromLocalPath(root, localPath string) string {
	rel, err := filepath.Rel(root, localPath)
	if err != nil {
		return ""
	}
	segments := strings.Split(rel, string(filepath.Separator))
	if len(segments) < 1 {
		return ""
	}
	host := segments[0]
	rest := strings.Join(segments[1:], "/")
	if rest == "" {
		rest = "/"
	}
	return (&url.URL{Scheme: "https", Host: host, Path: "/" + rest}).String()
}

func fileExists(fs *storage.Filesystem, rawURL string) bool {
	p := fs.PathForURL(rawURL)
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
