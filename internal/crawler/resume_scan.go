package crawler

import (
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// urlFromPath reconstructs a URL from a stored relative path
// (<host>/<path...>). Host-less and traversal-guarded entries are skipped.
func urlFromPath(rel string) string {
	segments := strings.Split(rel, string(filepath.Separator))
	if len(segments) == 0 {
		return ""
	}
	host := segments[0]
	if host == "" || host == "_unknown" || host == "_safe_path" || strings.HasPrefix(host, ".") {
		return ""
	}
	rest := strings.Join(segments[1:], "/")
	if rest == "" {
		rest = "/"
	} else {
		rest = "/" + rest
	}
	return (&url.URL{Scheme: "https", Host: host, Path: rest}).String()
}

func isRefAttr(k string) bool {
	switch k {
	case "href", "src", "action", "poster", "data":
		return true
	}
	if strings.HasPrefix(k, "data-") &&
		(strings.Contains(k, "src") || strings.Contains(k, "url") ||
			strings.Contains(k, "bg") || strings.Contains(k, "image") || strings.Contains(k, "lazy")) {
		return true
	}
	return false
}

// extractURLRefs collects every absolute http(s) URL referenced by a saved
// HTML page or stylesheet, resolving relative references against pageURL.
func extractURLRefs(content, pageURL string) []string {
	out := make(map[string]bool)
	page, err := url.Parse(pageURL)
	if err != nil {
		page = &url.URL{}
	}

	for _, m := range cssURLRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			if abs := resolveRef(page, m[1]); abs != "" {
				out[abs] = true
			}
		}
	}

	if strings.Contains(content, "<") {
		z := html.NewTokenizer(strings.NewReader(content))
		for {
			tt := z.Next()
			if tt == html.ErrorToken {
				break
			}
			if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
				continue
			}
			for {
				k, v, more := z.TagAttr()
				ks := strings.ToLower(string(k))
				vs := string(v)
				if ks == "srcset" {
					for _, part := range strings.Split(vs, ",") {
						up := strings.TrimSpace(strings.Split(strings.TrimSpace(part), " ")[0])
						if abs := resolveRef(page, up); abs != "" {
							out[abs] = true
						}
					}
				} else if isRefAttr(ks) {
					if abs := resolveRef(page, vs); abs != "" {
						out[abs] = true
					}
				}
				if !more {
					break
				}
			}
		}
	}

	res := make([]string, 0, len(out))
	for u := range out {
		res = append(res, u)
	}
	return res
}

func resolveRef(base *url.URL, ref string) string {
	ref = strings.TrimSpace(strings.Trim(ref, `"'`))
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "data:") ||
		strings.HasPrefix(ref, "javascript:") || strings.HasPrefix(ref, "mailto:") ||
		strings.HasPrefix(ref, "tel:") || strings.HasPrefix(ref, "blob:") {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	var abs *url.URL
	if u.Scheme == "" {
		abs = base.ResolveReference(u)
	} else {
		abs = u
	}
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}
