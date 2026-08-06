package localize

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	stdhtml "html"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var wsReplayTagPattern = regexp.MustCompile(`(?i)<script[^>]*\bsrc="[^"]*ws-replay\.js"[^>]*>\s*</script>`)

// Rewriter localizes HTML and CSS references using a complete URL-to-file
// mapping. Every relative path is recomputed from the target file's own
// directory; stored cross-directory paths are never reused.
type Rewriter struct {
	urlToLocal map[string]string
	pageURLs   map[string]string
	localRoot  string
}

// NewRewriter builds a Rewriter over a complete mapping.
func NewRewriter(urlToLocal, pageURLs map[string]string, localRoot string) *Rewriter {
	return &Rewriter{urlToLocal: urlToLocal, pageURLs: pageURLs, localRoot: localRoot}
}

var urlAttrs = map[string]bool{
	"href":   true,
	"src":    true,
	"action": true,
	"poster": true,
	"data":   true,
}

var dataURLAttrs = map[string]bool{
	"data-src":         true,
	"data-srcset":      true,
	"data-lazy-src":    true,
	"data-original":    true,
	"data-image":       true,
	"data-bg":          true,
	"data-background":  true,
	"data-href":        true,
	"data-url":         true,
	"data-srcpath":     true,
	"data-request":     true,
	"data-endpoint":    true,
	"data-lazy":        true,
	"data-delayed-url": true,
	"data-settings":    true,
}

func isURLAttr(key string) bool {
	return urlAttrs[key] || dataURLAttrs[key]
}

// RewriteHTML localizes every reference in an HTML document.
func (r *Rewriter) RewriteHTML(data []byte, htmlPath string) []byte {
	htmlDir := filepath.Dir(htmlPath)
	pageURL := r.pageURLs[htmlPath]

	z := html.NewTokenizer(bytes.NewReader(data))
	var buf bytes.Buffer
	buf.Grow(len(data))

	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() == io.EOF {
				break
			}
			buf.Write(z.Raw())
			continue
		}

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tagAtom := atom.Lookup(name)

			if tagAtom == atom.Style && tt == html.StartTagToken {
				inStyle = true
			}

			var attrs []attrPair
			if hasAttr {
				for {
					k, v, more := z.TagAttr()
					attrs = append(attrs, attrPair{key: string(k), val: string(v)})
					if !more {
						break
					}
				}
			}

			if tagAtom == atom.Base {
				continue
			}

			var rewrites []attrRewrite
			isCSPMeta := false
			for _, a := range attrs {
				if a.key == "http-equiv" && strings.EqualFold(a.val, "content-security-policy") {
					isCSPMeta = true
					break
				}
			}

			for _, a := range attrs {
				k, v := a.key, a.val
				switch {
				case k == "integrity":
					rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: ""})
				case isCSPMeta && k == "content":
					rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: "default-src * 'unsafe-inline' 'unsafe-eval' data: blob:;"})
				case k == "http-equiv" && strings.EqualFold(v, "content-security-policy"):
					rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: "Content-Security-Policy"})
				case k == "srcset" || k == "data-srcset" || k == "imagesrcset":
					if nv, ok := r.rewriteSrcset(v, pageURL, htmlDir); ok {
						rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: nv})
					}
				case k == "imagesrc":
					if nv, ok := r.rewriteAttrValue(v, pageURL, htmlDir); ok {
						rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: nv})
					}
				case k == "style":
					nv := r.rewriteCSSRefs(v, pageURL, htmlDir)
					if nv != v {
						rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: nv})
					}
				case isURLAttr(k):
					if nv, ok := r.rewriteAttrValue(v, pageURL, htmlDir); ok {
						rewrites = append(rewrites, attrRewrite{key: k, oldVal: v, newVal: nv})
					}
				}
			}

			if tagAtom == atom.Meta {
				prop := ""
				for _, a := range attrs {
					if a.key == "property" || a.key == "name" {
						prop = strings.ToLower(a.val)
						break
					}
				}
				if prop == "og:image" || prop == "twitter:image" {
					for i := range attrs {
						if attrs[i].key == "content" {
							if nv, ok := r.rewriteAttrValue(attrs[i].val, pageURL, htmlDir); ok {
								rewrites = append(rewrites, attrRewrite{key: "content", oldVal: attrs[i].val, newVal: nv})
							}
						}
					}
				}
			}

			if tt == html.SelfClosingTagToken && len(rewrites) == 0 {
				buf.Write(z.Raw())
			} else {
				writeTag(&buf, name, attrs, rewrites, tt)
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			if atom.Lookup(name) == atom.Style {
				inStyle = false
			}
			buf.Write(z.Raw())

		case html.TextToken:
			if inStyle {
				buf.WriteString(r.rewriteCSSRefs(string(z.Text()), pageURL, htmlDir))
			} else {
				buf.Write(z.Raw())
			}

		default:
			buf.Write(z.Raw())
		}
	}

	result := buf.Bytes()

	result = wsReplayTagPattern.ReplaceAll(result, nil)
	if r.localRoot != "" {
		replayPath := filepath.Join(r.localRoot, "ws-replay.js")
		if _, err := os.Stat(replayPath); err == nil {
			rel := filepath.ToSlash(filepath.Clean(relPath(htmlDir, replayPath)))
			if rel == "." {
				rel = "ws-replay.js"
			}
			result = injectBeforeBody(result, `<script src="`+stdhtml.EscapeString(rel)+`"></script>`)
		}
	}

	return result
}

// RewriteCSS localizes every url() and @import reference in a stylesheet.
func (r *Rewriter) RewriteCSS(data []byte, cssPath string) []byte {
	cssDir := filepath.Dir(cssPath)
	cssURL := r.pageURLs[cssPath]
	return []byte(r.rewriteCSSRefs(string(data), cssURL, cssDir))
}

// rewriteAttrValue localizes a single attribute value, returning the new value.
func (r *Rewriter) rewriteAttrValue(v, pageURL, dir string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "#") || strings.HasPrefix(v, "javascript:") ||
		strings.HasPrefix(v, "mailto:") || strings.HasPrefix(v, "tel:") ||
		strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "blob:") {
		return "", false
	}
	if rel, ok := r.localize(v, pageURL, dir); ok {
		return rel, true
	}
	return "", false
}

// rewriteSrcset localizes each URL entry of a srcset-like attribute.
func (r *Rewriter) rewriteSrcset(v, pageURL, dir string) (string, bool) {
	parts := strings.Split(v, ",")
	changed := false
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		urlPart := strings.Split(part, " ")[0]
		if urlPart == "" || strings.HasPrefix(urlPart, "data:") {
			continue
		}
		if rel, ok := r.localize(urlPart, pageURL, dir); ok {
			parts[i] = rel + strings.TrimPrefix(part, urlPart)
			changed = true
		}
	}
	if !changed {
		return "", false
	}
	return strings.Join(parts, ", "), true
}
