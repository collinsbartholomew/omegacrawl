package localize

import (
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// isBinaryHTML reports whether the file at path looks like binary content
// rather than an HTML document (e.g. an image misnamed index.html).
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

// isHTMLPagePath reports whether a local path is an HTML page file.
func isHTMLPagePath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// isCSSPath reports whether a local path is a stylesheet.
func isCSSPath(p string) bool {
	return strings.EqualFold(filepath.Ext(p), ".css")
}

// extractAssetURLs returns the absolute http(s) asset URLs referenced by a
// page, including meta image tags, inline style url() references and srcset.
func extractAssetURLs(pageHTML []byte, pageURL string) map[string]bool {
	urls := make(map[string]bool)
	z := html.NewTokenizer(strings.NewReader(string(pageHTML)))
	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			name, hasAttr := z.TagName()
			tagName := strings.ToLower(string(name))
			if tt == html.StartTagToken && tagName == "style" {
				inStyle = true
			}

			var attrs []struct{ k, v string }
			if hasAttr {
				for {
					k, v, more := z.TagAttr()
					attrs = append(attrs, struct{ k, v string }{string(k), string(v)})
					if !more {
						break
					}
				}
			}

			if tagName == "meta" {
				prop, contentVal := "", ""
				for _, attr := range attrs {
					switch attr.k {
					case "property", "name":
						prop = strings.ToLower(attr.v)
					case "content":
						contentVal = attr.v
					}
				}
				if (prop == "og:image" || prop == "twitter:image") && contentVal != "" &&
					(strings.HasPrefix(contentVal, "http://") || strings.HasPrefix(contentVal, "https://")) {
					urls[contentVal] = true
				}
			}

			for _, attr := range attrs {
				ak, av := attr.k, attr.v
				if av == "" || strings.HasPrefix(av, "#") || strings.HasPrefix(av, "javascript:") ||
					strings.HasPrefix(av, "mailto:") || strings.HasPrefix(av, "tel:") || strings.HasPrefix(av, "data:") {
					continue
				}

				if ak == "style" {
					for _, cssURL := range extractCSSURLs(av) {
						if abs := resolveAbsolute(pageURL, cssURL); abs != "" {
							urls[abs] = true
						}
					}
					continue
				}

				var candidates []string
				switch ak {
				case "src", "poster", "data":
					candidates = []string{av}
				case "srcset":
					for _, part := range strings.Split(av, ",") {
						candidates = append(candidates, strings.TrimSpace(strings.Split(strings.TrimSpace(part), " ")[0]))
					}
				case "href", "action":
					abs := resolveURL(pageURL, av)
					if isHTMLPageURL(abs) {
						continue
					}
					candidates = []string{av}
				default:
					if strings.HasPrefix(ak, "data-") && (strings.Contains(ak, "src") || strings.Contains(ak, "url") ||
						strings.Contains(ak, "bg") || strings.Contains(ak, "image") || strings.Contains(ak, "lazy")) {
						candidates = []string{av}
					}
				}

				for _, c := range candidates {
					if c == "" {
						continue
					}
					if !strings.HasPrefix(c, "http://") && !strings.HasPrefix(c, "https://") {
						continue
					}
					urls[c] = true
				}
			}
		} else if tt == html.TextToken {
			if inStyle {
				for _, cssURL := range extractCSSURLs(string(z.Text())) {
					if abs := resolveAbsolute(pageURL, cssURL); abs != "" {
						urls[abs] = true
					}
				}
			}
		} else if tt == html.EndTagToken {
			name, _ := z.TagName()
			if strings.ToLower(string(name)) == "style" {
				inStyle = false
			}
		}
	}
	return urls
}

// extractCSSURLs parses url(...) references from CSS text, correctly handling
// backslash-escaped characters (e.g. \( and \)). Escapes are decoded.
func extractCSSURLs(css string) []string {
	var urls []string
	lower := strings.ToLower(css)
	for {
		idx := strings.Index(lower, "url(")
		if idx < 0 {
			break
		}
		rest := css[idx+4:]
		depth := 1
		var sb strings.Builder
		i := 0
		for i < len(rest) && depth > 0 {
			ch := rest[i]
			if ch == '\\' && i+1 < len(rest) {
				sb.WriteByte(rest[i+1])
				i += 2
				continue
			}
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
			sb.WriteByte(ch)
			i++
		}
		css = rest[i+1:]
		lower = lower[idx+4+i+1:]
		val := strings.TrimSpace(sb.String())
		val = strings.Trim(val, `"'`)
		val = strings.TrimSpace(val)
		if val != "" {
			urls = append(urls, val)
		}
	}
	return urls
}

func resolveAbsolute(pageURL, cssURL string) string {
	if cssURL == "" || strings.HasPrefix(cssURL, "data:") {
		return ""
	}
	abs := resolveURL(pageURL, cssURL)
	if strings.HasPrefix(abs, "http://") || strings.HasPrefix(abs, "https://") {
		return abs
	}
	return ""
}

// isHTMLPageURL reports whether a URL points at an HTML page rather than a
// static asset, based on the path extension. Extensionless paths are pages.
func isHTMLPageURL(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".html", ".htm", ".php", ".asp", ".aspx", ".jsp", ".jspx", ".cfm":
		return true
	}
	return ext == ""
}
