package repair

import (
	"net/url"
	"path"
	"strings"

	"github.com/user/clone/internal/rewrite"
	"golang.org/x/net/html"
)

// extractAssetURLs returns the absolute http(s) URLs referenced by a page
// that are likely static assets rather than navigation endpoints.
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
				var prop, contentVal string
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

						if !strings.HasPrefix(cssURL, "http://") && !strings.HasPrefix(cssURL, "https://") {
							continue
						}
						if u := resolveAbsolute(pageURL, cssURL); u != "" {
							urls[u] = true
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

					abs := rewrite.ResolveURL(pageURL, av)
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
					abs := rewrite.ResolveURL(pageURL, c)
					if strings.HasPrefix(abs, "http://") || strings.HasPrefix(abs, "https://") {
						urls[abs] = true
					}
				}
			}
		} else if tt == html.TextToken {
			if inStyle {
				for _, cssURL := range extractCSSURLs(string(z.Text())) {
					if !strings.HasPrefix(cssURL, "http://") && !strings.HasPrefix(cssURL, "https://") {
						continue
					}
					if u := resolveAbsolute(pageURL, cssURL); u != "" {
						urls[u] = true
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
// backslash-escaped characters (e.g. \( and \)) which appear in Webflow asset
// names. Each returned URL has CSS escapes decoded.
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

// resolveAbsolute resolves cssURL against pageURL, returning the absolute
// http(s) URL or an empty string if it is relative or not an asset candidate.
func resolveAbsolute(pageURL, cssURL string) string {
	if cssURL == "" || strings.HasPrefix(cssURL, "data:") {
		return ""
	}
	abs := rewrite.ResolveURL(pageURL, cssURL)
	if strings.HasPrefix(abs, "http://") || strings.HasPrefix(abs, "https://") {
		return abs
	}
	return ""
}

// isHTMLPageURL reports whether a URL points at an HTML page rather than a
// static asset, based on the path extension. Extensionless paths are treated
// as pages.
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
