package localize

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// localize maps a reference (absolute or relative to pageURL) to a relative
// path from dir, if the referenced URL exists in the mapping.
func (r *Rewriter) localize(ref, pageURL, dir string) (string, bool) {
	var abs string
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		abs = ref
	} else if isProtocolRelativeURL(ref) {
		abs = "https://" + ref
	} else if pageURL != "" {
		abs = resolveURL(pageURL, ref)
	}
	if abs == "" {
		return "", false
	}
	lp, ok := r.urlToLocal[abs]
	if !ok {
		// Try unescaped path variant.
		if d, err := url.PathUnescape(abs); err == nil && d != abs {
			lp, ok = r.urlToLocal[d]
		}
	}
	if !ok {
		// Fallback: derive expected local path from URL (for legacy clones).
		if lp := r.pathForURL(abs); lp != "" {
			if fi, err := os.Stat(lp); err == nil && !fi.IsDir() {
				return filepath.ToSlash(filepath.Clean(relPath(dir, lp))), true
			}
		}
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relPath(dir, lp))), true
}

// pathForURL derives the absolute local path a URL would have been saved to,
// mirroring the storage layer's filename rules. Returns "" for non-http URLs.
func (r *Rewriter) pathForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	cleanPath := u.Path
	if cleanPath == "" || cleanPath == "/" {
		cleanPath = "/index.html"
	}
	decoded, err := url.PathUnescape(cleanPath)
	if err != nil {
		decoded = cleanPath
	}
	if strings.HasSuffix(decoded, "/") {
		decoded += "index.html"
	}
	decoded = strings.TrimPrefix(decoded, "/")
	baseName := decoded
	if idx := strings.LastIndex(decoded, "/"); idx >= 0 {
		baseName = decoded[idx+1:]
	}
	if !strings.Contains(baseName, ".") {
		decoded += "/index.html"
	}
	if strings.Contains(decoded, "..") {
		return ""
	}
	if u.RawQuery != "" {
		ext := filepath.Ext(decoded)
		base := strings.TrimSuffix(decoded, ext)
		safeQuery := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "?", "_", "&", "_").Replace(u.RawQuery)
		decoded = base + "_" + safeQuery + ext
	}
	return filepath.Join(r.localRoot, host, filepath.FromSlash(decoded))
}

// isProtocolRelativeURL detects URLs that start with a domain name but lack
// a scheme (e.g., "cdn.example.com/path", "example.com/path").
// These are treated as https:// URLs.
func isProtocolRelativeURL(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	// Must not start with scheme, slash, or special prefix
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "#") ||
		strings.HasPrefix(ref, "javascript:") || strings.HasPrefix(ref, "mailto:") ||
		strings.HasPrefix(ref, "tel:") || strings.HasPrefix(ref, "data:") ||
		strings.HasPrefix(ref, "blob:") {
		return false
	}
	// Must contain at least one dot (domain-like) and no spaces
	if !strings.Contains(ref, ".") || strings.Contains(ref, " ") {
		return false
	}
	// First segment before '/' or '?' or '#' should look like a hostname
	firstSeg := ref
	for _, sep := range []string{"/", "?", "#"} {
		if idx := strings.Index(firstSeg, sep); idx >= 0 {
			firstSeg = firstSeg[:idx]
			break
		}
	}
	// Hostname should have at least one dot and valid characters
	if !strings.Contains(firstSeg, ".") {
		return false
	}
	// Basic hostname validation: alphanumeric, dots, hyphens
	for _, r := range firstSeg {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == ':' { // IPv6 literals in brackets not handled here
			continue
		}
		return false
	}
	return true
}
