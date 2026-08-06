package rewrite

import (
	"bytes"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	stdhtml "html"

	"go.uber.org/zap"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/user/clone/internal/util"
)

var (
	// cssURLPattern matches a CSS url(...) token. The inner group consumes
	// backslash-escaped characters (\\( \\) etc.) so a URL containing escaped
	// parentheses is captured as one token instead of being truncated at the
	// first closing paren.
	cssURLPattern    = regexp.MustCompile(`url\(\s*["']?((?:\\.|[^"')])+)["']?\s*\)`)
	cssImportPattern = regexp.MustCompile(`@import\s+(?:url\()?\s*["']?((?:\\.|[^"')])+)["']?\s*\)?;`)
	fontFaceBlock    = regexp.MustCompile(`@font-face\s*\{[^}]*\}`)
	srcValuePattern  = regexp.MustCompile(`src:\s*([^;]+);`)
	scriptURLPattern = regexp.MustCompile(`"((?:https?:)?//[^"]+|/[^"]+|(?:\.\./|./)[^"]+)"|'((?:https?:)?//[^']+|/[^']+|(?:\.\./|./)[^']+)'`)
	dataURIPattern   = regexp.MustCompile(`data:([^;,]+)(?:;base64)?,([^"']+)`)
)

// RewriteHTML rewrites URLs in the given HTML content to local relative paths.
func (r *Rewriter) RewriteHTML(htmlContent []byte, htmlLocalPath string) []byte {
	htmlDir := filepath.Dir(htmlLocalPath)

	r.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range r.urlToLocal {
		mappings[k] = v
	}
	absToRel := make(map[string]string)
	for k, v := range r.absoluteToRel {
		absToRel[k] = v
	}
	baseURL := r.baseURL
	r.mu.RUnlock()

	util.LogDebug("RewriteHTML called", zap.String("htmlLocalPath", htmlLocalPath), zap.Int("mappings", len(mappings)), zap.Int("absToRel", len(absToRel)), zap.String("baseURL", baseURL))

	if len(mappings) == 0 && len(absToRel) == 0 {
		util.LogDebug("rewrite: no mappings, skipping rewrite", zap.String("html", htmlLocalPath))
		result := rewriteBaseTag(htmlContent)
		return injectWSReplayScript(result)
	}
	util.LogDebug("rewrite: mappings count", zap.Int("mappings", len(mappings)), zap.Int("absToRel", len(absToRel)), zap.String("baseURL", baseURL))

	sortedURLs := make([]string, 0, len(mappings))
	for origURL := range mappings {
		sortedURLs = append(sortedURLs, origURL)
	}
	sort.Slice(sortedURLs, func(i, j int) bool {
		return len(sortedURLs[i]) > len(sortedURLs[j])
	})

	pathCache := make(map[string]string, len(mappings))
	for origURL, localPath := range mappings {
		relPath, err := filepath.Rel(htmlDir, localPath)
		if err != nil {
			relPath = localPath
		}
		pathCache[origURL] = filepath.ToSlash(relPath)
	}

	absRelCache := make(map[string]string, len(absToRel))
	for absURL, relPath := range absToRel {
		absRelCache[absURL] = relPath
	}

	result := rewriteHTMLTokens(htmlContent, htmlDir, baseURL, mappings, pathCache, absRelCache)

	result = replaceQuotedURLs(result, sortedURLs, pathCache)
	result = replaceAbsoluteURLs(result, absRelCache)

	result = rewriteBaseTag(result)

	result = injectWSReplayScript(result)

	return result
}

func injectWSReplayScript(html []byte) []byte {
	if bytes.Contains(html, []byte(`src="ws-replay.js"`)) {
		return html
	}
	closingBody := []byte("</body>")
	if idx := bytes.LastIndex(html, closingBody); idx != -1 {
		injection := []byte(`<script src="ws-replay.js"></script>`)
		result := make([]byte, 0, len(html)+len(injection))
		result = append(result, html[:idx]...)
		result = append(result, injection...)
		result = append(result, html[idx:]...)
		return result
	}
	closingHTML := []byte("</html>")
	if idx := bytes.LastIndex(html, closingHTML); idx != -1 {
		injection := []byte(`<script src="ws-replay.js"></script>`)
		result := make([]byte, 0, len(html)+len(injection))
		result = append(result, html[:idx]...)
		result = append(result, injection...)
		result = append(result, html[idx:]...)
		return result
	}
	return html
}

func rewriteBaseTag(htmlContent []byte) []byte {
	z := html.NewTokenizer(bytes.NewReader(htmlContent))
	var buf bytes.Buffer
	buf.Grow(len(htmlContent))

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if atom.Lookup(name) == atom.Base {
				var attrs []attrPair
				if hasAttr {
					for {
						k, v, more := z.TagAttr()
						attrs = append(attrs, attrPair{k, v})
						if !more {
							break
						}
					}
				}
				writeTag(&buf, name, attrs,
					[]attrRewrite{{key: "href", oldVal: "", newVal: "."}}, tt)
			} else {
				buf.Write(z.Raw())
			}
		default:
			buf.Write(z.Raw())
		}
	}

	if buf.Len() == 0 {
		return htmlContent
	}
	return buf.Bytes()
}

type attrRewrite struct {
	key, oldVal, newVal string
}

type attrPair struct {
	key, val []byte
}

func rewriteHTMLTokens(
	input []byte,
	htmlDir string,
	baseURL string,
	mappings map[string]string,
	pathCache map[string]string,
	absRelCache map[string]string,
) []byte {
	z := html.NewTokenizer(bytes.NewReader(input))
	var buf bytes.Buffer
	buf.Grow(len(input))

	inScriptOrStyle := false

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
			nameStr := strings.ToLower(string(name))
			tagAtom := atom.Lookup(name)
			if (tagAtom == atom.Script || tagAtom == atom.Style) && tt == html.StartTagToken {
				inScriptOrStyle = true
			}

			var attrs []attrPair
			var rewrites []attrRewrite
			isCSPMeta := false

			if hasAttr {

				for {
					k, v, more := z.TagAttr()
					attrs = append(attrs, attrPair{k, v})
					if !more {
						break
					}
				}

				for _, attr := range attrs {
					keyStr := string(attr.key)
					valStr := string(attr.val)
					if keyStr == "http-equiv" && strings.EqualFold(valStr, "content-security-policy") {
						isCSPMeta = true
						break
					}
				}

				for _, attr := range attrs {
					keyStr := string(attr.key)
					valStr := string(attr.val)

					if isURLAttr([]byte(keyStr)) {
						util.LogDebug("rewriteHTMLTokens: checking attr",
							zap.String("tag", nameStr),
							zap.String("attr", keyStr),
							zap.String("val", valStr),
							zap.String("baseURL", baseURL),
						)
					}

					if keyStr == "integrity" {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, ""})
					} else if isCSPMeta && keyStr == "content" && nameStr == "meta" {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, "default-src * 'unsafe-inline' 'unsafe-eval' data: blob:;"})
					} else if keyStr == "http-equiv" && strings.EqualFold(valStr, "content-security-policy") {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, "Content-Security-Policy"})
					} else if isURLAttr([]byte(keyStr)) {
						if newVal, ok := rewriteAttrVal(valStr, htmlDir, baseURL, mappings, pathCache, absRelCache); ok {
							rewrites = append(rewrites, attrRewrite{keyStr, valStr, newVal})
						}
					}

					if keyStr == "style" {
						newVal := rewriteInlineCSSURLs([]byte(valStr), htmlDir, baseURL, mappings, pathCache, absRelCache)
						if string(newVal) != valStr {
							rewrites = append(rewrites, attrRewrite{keyStr, valStr, string(newVal)})
						}
					}
				}
			}

			var needsRewrite bool
			if len(rewrites) > 0 {
				needsRewrite = true
			}

			if tt == html.SelfClosingTagToken && !needsRewrite {
				buf.Write(z.Raw())
			} else {
				writeTag(&buf, name, attrs, rewrites, tt)
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tagAtom := atom.Lookup(name)
			if tagAtom == atom.Script || tagAtom == atom.Style {
				inScriptOrStyle = false
			}
			buf.Write(z.Raw())

		case html.TextToken:
			if inScriptOrStyle {
				buf.Write(z.Raw())
			} else {
				text := z.Text()
				rewritten := rewriteScriptStyleURLs(text, baseURL, mappings, pathCache, absRelCache)
				buf.Write(rewritten)
			}

		default:
			buf.Write(z.Raw())
		}
	}

	return buf.Bytes()
}

func writeTag(buf *bytes.Buffer, tagName []byte, attrs []attrPair, rewrites []attrRewrite, tt html.TokenType) {
	buf.WriteByte('<')
	buf.Write(tagName)

	rewriteMap := make(map[string]string, len(rewrites))
	for _, r := range rewrites {
		rewriteMap[r.key] = r.newVal
	}

	for _, attr := range attrs {
		keyStr := string(attr.key)
		if newVal, ok := rewriteMap[keyStr]; ok {
			writeAttr(buf, attr.key, []byte(newVal))
		} else {
			writeAttr(buf, attr.key, attr.val)
		}
	}

	if tt == html.SelfClosingTagToken {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
}

func writeAttr(buf *bytes.Buffer, key, val []byte) {
	buf.WriteByte(' ')
	buf.Write(key)
	buf.WriteString(`="`)
	buf.WriteString(stdhtml.EscapeString(string(val)))
	buf.WriteByte('"')
}
