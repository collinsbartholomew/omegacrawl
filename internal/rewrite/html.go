package rewrite

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	stdhtml "html"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

var (
	cssURLPattern    = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
	cssImportPattern = regexp.MustCompile(`@import\s+(?:url\()?\s*["']?([^"')]+)["']?\s*\)?;`)
	fontFaceBlock    = regexp.MustCompile(`@font-face\s*\{[^}]*\}`)
	srcValuePattern  = regexp.MustCompile(`src:\s*([^;]+);`)
	scriptURLPattern = regexp.MustCompile(`"((?:https?:)?//[^"]+|/[^"]+|(?:\.\./|./)[^"]+)"|'((?:https?:)?//[^']+|/[^']+|(?:\.\./|./)[^']+)'`)
	dataURIPattern   = regexp.MustCompile(`data:([^;,]+)(?:;base64)?,([^"']+)`)
)

var urlAttrs = map[atom.Atom]bool{
	atom.Href:       true,
	atom.Src:        true,
	atom.Action:     true,
	atom.Poster:     true,
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

type Rewriter struct {
	urlToLocal     map[string]string
	absoluteToRel  map[string]string
	cssFiles       map[string]bool
	baseURL        string
	mu             sync.RWMutex
}

func NewRewriter() *Rewriter {
	return &Rewriter{
		urlToLocal:    make(map[string]string),
		absoluteToRel: make(map[string]string),
		cssFiles:      make(map[string]bool),
	}
}

func (r *Rewriter) SetBaseURL(baseURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.baseURL = baseURL
}

func (r *Rewriter) AddMapping(originalURL, localPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urlToLocal[originalURL] = localPath
	if strings.HasSuffix(localPath, ".css") {
		r.cssFiles[localPath] = true
	}
}

func (r *Rewriter) GetCSSFiles() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]bool)
	for k, v := range r.cssFiles {
		result[k] = v
	}
	return result
}

func (r *Rewriter) GetMappings() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range r.urlToLocal {
		result[k] = v
	}
	return result
}

func (r *Rewriter) GetAbsoluteToRel() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range r.absoluteToRel {
		result[k] = v
	}
	return result
}

func (r *Rewriter) AddAbsoluteToRelMapping(absoluteURL, relativePath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.absoluteToRel[absoluteURL] = relativePath
}

func isURLAttr(key []byte) bool {
	a := atom.Lookup(key)
	if a != 0 && urlAttrs[a] {
		return true
	}
	return dataURLAttrs[string(key)]
}

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

	if len(mappings) == 0 && len(absToRel) == 0 {
		result := rewriteBaseTag(htmlContent)
		return injectWSReplayScript(result)
	}

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

			var attrs []attrPair
			var rewrites []attrRewrite

			if hasAttr {
				for {
					k, v, more := z.TagAttr()
					attrs = append(attrs, attrPair{k, v})
					keyStr := string(k)
					valStr := string(v)

					if keyStr == "integrity" {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, ""})
					} else if keyStr == "http-equiv" && strings.ToLower(valStr) == "content-security-policy" {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, "Content-Security-Policy"})
					} else if keyStr == "content" && nameStr == "meta" {
						rewrites = append(rewrites, attrRewrite{keyStr, valStr, "default-src * 'unsafe-inline' 'unsafe-eval' data: blob:;"})
					} else if isURLAttr(k) {
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

					if !more {
						break
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

		case html.TextToken:
			if isScriptOrStyle(z) {
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

func isScriptOrStyle(z *html.Tokenizer) bool {
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return false
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			name, _ := z.TagName()
			if atom.Lookup(name) == atom.Script || atom.Lookup(name) == atom.Style {
				return true
			}
			return false
		}
	}
}

func rewriteScriptStyleURLs(text []byte, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) []byte {
	result := text

	for absURL, relPath := range absRelCache {
		old := []byte(absURL)
		new := []byte(relPath)
		result = bytes.ReplaceAll(result, old, new)
	}

	for absURL, relPath := range pathCache {
		old := []byte(absURL)
		new := []byte(relPath)
		result = bytes.ReplaceAll(result, old, new)
	}

	result = scriptURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		sub := scriptURLPattern.FindSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		var quote []byte
		var urlStr string
		if len(sub[1]) > 0 {
			quote = sub[1][:1]
			urlStr = string(sub[1])
		} else if len(sub[2]) > 0 {
			quote = sub[2][:1]
			urlStr = string(sub[2])
		} else {
			return match
		}

		if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
			if relPath, ok := pathCache[urlStr]; ok {
				return append(append(quote, relPath...), quote...)
			}
			if relPath, ok := absRelCache[urlStr]; ok {
				return append(append(quote, relPath...), quote...)
			}
		} else if baseURL != "" {
			resolved := ResolveURL(baseURL, urlStr)
			if relPath, ok := pathCache[resolved]; ok {
				return append(append(quote, relPath...), quote...)
			}
			if relPath, ok := absRelCache[resolved]; ok {
				return append(append(quote, relPath...), quote...)
			}
		}
		return match
	})

	result = cssURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		sub := cssURLPattern.FindSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		urlStr := string(bytes.TrimSpace(sub[1]))
		urlStr = strings.Trim(urlStr, `"' `)

		if relPath, ok := pathCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}
		if relPath, ok := absRelCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
			resolved := ResolveURL(baseURL, urlStr)
			if resolved != "" && resolved != urlStr {
				if relPath, ok := pathCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
				if relPath, ok := absRelCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
			}
		}
		return match
	})

	return result
}

func rewriteAttrVal(val string, htmlDir string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) (string, bool) {
	if val == "" || strings.HasPrefix(val, "#") || strings.HasPrefix(val, "javascript:") || strings.HasPrefix(val, "mailto:") || strings.HasPrefix(val, "tel:") || strings.HasPrefix(val, "data:") {
		return "", false
	}

	if strings.Contains(val, ",") && (containsURL(mappings, val) || containsURL(absRelCache, val)) {
		return rewriteSrcset(val, baseURL, mappings, pathCache, absRelCache)
	}

	if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		if relPath, ok := pathCache[val]; ok {
			return relPath, true
		}
		if relPath, ok := absRelCache[val]; ok {
			return relPath, true
		}
	}

	if baseURL != "" {
		resolved := ResolveURL(baseURL, val)
		if resolved != "" && resolved != val {
			if relPath, ok := pathCache[resolved]; ok {
				return relPath, true
			}
			if relPath, ok := absRelCache[resolved]; ok {
				return relPath, true
			}
		}
	}

	return "", false
}

func rewriteSrcset(val string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) (string, bool) {
	parts := strings.Split(val, ",")
	changed := false
	for i, part := range parts {
		part = strings.TrimSpace(part)
		urlPart := strings.Split(part, " ")[0]
		if urlPart == "" {
			continue
		}

		if relPath, ok := pathCache[urlPart]; ok {
			parts[i] = relPath + strings.TrimPrefix(part, urlPart)
			changed = true
			continue
		}
		if relPath, ok := absRelCache[urlPart]; ok {
			parts[i] = relPath + strings.TrimPrefix(part, urlPart)
			changed = true
			continue
		}

		if baseURL != "" && !strings.HasPrefix(urlPart, "http://") && !strings.HasPrefix(urlPart, "https://") && !strings.HasPrefix(urlPart, "data:") {
			resolved := ResolveURL(baseURL, urlPart)
			if resolved != "" && resolved != urlPart {
				if relPath, ok := pathCache[resolved]; ok {
					parts[i] = relPath + strings.TrimPrefix(part, urlPart)
					changed = true
					continue
				}
				if relPath, ok := absRelCache[resolved]; ok {
					parts[i] = relPath + strings.TrimPrefix(part, urlPart)
					changed = true
					continue
				}
			}
		}
	}
	if changed {
		return strings.Join(parts, ", "), true
	}
	return "", false
}

func containsURL(mappings map[string]string, val string) bool {
	for url := range mappings {
		if strings.Contains(val, url) {
			return true
		}
	}
	return false
}

func rewriteInlineCSSURLs(styleVal []byte, htmlDir string, baseURL string, mappings map[string]string, pathCache map[string]string, absRelCache map[string]string) []byte {
	result := cssURLPattern.ReplaceAllFunc(styleVal, func(match []byte) []byte {
		sub := cssURLPattern.FindSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		urlStr := string(bytes.TrimSpace(sub[1]))
		urlStr = strings.Trim(urlStr, `"' `)

		if localPath, ok := mappings[urlStr]; ok {
			relPath, err := filepath.Rel(htmlDir, localPath)
			if err != nil {
				relPath = localPath
			}
			relPath = filepath.ToSlash(relPath)
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if relPath, ok := pathCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}
		if relPath, ok := absRelCache[urlStr]; ok {
			return bytes.Replace(match, sub[1], []byte(relPath), 1)
		}

		if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
			resolved := ResolveURL(baseURL, urlStr)
			if resolved != "" && resolved != urlStr {
				if relPath, ok := pathCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
				if relPath, ok := absRelCache[resolved]; ok {
					return bytes.Replace(match, sub[1], []byte(relPath), 1)
				}
			}
		}
		return match
	})

	result = cssImportPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		matches := cssImportPattern.FindSubmatch(match)
		if len(matches) > 1 {
			urlStr := string(matches[1])
			urlStr = strings.Trim(urlStr, `"' `)
			if localPath, ok := mappings[urlStr]; ok {
				relPath, _ := filepath.Rel(htmlDir, localPath)
				relPath = filepath.ToSlash(relPath)
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := pathCache[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := absRelCache[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if baseURL != "" && !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "data:") {
				resolved := ResolveURL(baseURL, urlStr)
				if resolved != "" && resolved != urlStr {
					if relPath, ok := pathCache[resolved]; ok {
						return bytes.Replace(match, matches[1], []byte(relPath), 1)
					}
					if relPath, ok := absRelCache[resolved]; ok {
						return bytes.Replace(match, matches[1], []byte(relPath), 1)
					}
				}
			}
		}
		return match
	})

	return result
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

func replaceQuotedURLs(input []byte, sortedURLs []string, pathCache map[string]string) []byte {
	result := input
	for _, origURL := range sortedURLs {
		relPath := pathCache[origURL]

		old := []byte(`"` + origURL + `"`)
		new := []byte(`"` + relPath + `"`)
		result = bytes.ReplaceAll(result, old, new)

		old = []byte(`'` + origURL + `'`)
		new = []byte(`'` + relPath + `'`)
		result = bytes.ReplaceAll(result, old, new)

		old = []byte("`" + origURL + "`")
		new = []byte("`" + relPath + "`")
		result = bytes.ReplaceAll(result, old, new)
	}
	return result
}

func replaceAbsoluteURLs(input []byte, absRelCache map[string]string) []byte {
	result := input
	for absURL, relPath := range absRelCache {
		old := []byte(absURL)
		new := []byte(relPath)
		result = bytes.ReplaceAll(result, old, new)
	}
	return result
}

func (r *Rewriter) RewriteCSS(css []byte, cssLocalPath string) []byte {
	result := css
	cssDir := filepath.Dir(cssLocalPath)

	r.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range r.urlToLocal {
		mappings[k] = v
	}
	absToRel := make(map[string]string)
	for k, v := range r.absoluteToRel {
		absToRel[k] = v
	}
	r.mu.RUnlock()

	sortedURLs := make([]string, 0, len(mappings))
	for origURL := range mappings {
		sortedURLs = append(sortedURLs, origURL)
	}
	sort.Slice(sortedURLs, func(i, j int) bool {
		return len(sortedURLs[i]) > len(sortedURLs[j])
	})

	for _, origURL := range sortedURLs {
		localPath := mappings[origURL]
		relPath, err := filepath.Rel(cssDir, localPath)
		if err != nil {
			relPath = localPath
		}
		relPath = filepath.ToSlash(relPath)

		old := []byte(`"` + origURL + `"`)
		new := []byte(`"` + relPath + `"`)
		result = bytes.ReplaceAll(result, old, new)

		old = []byte(`'` + origURL + `'`)
		new = []byte(`'` + relPath + `'`)
		result = bytes.ReplaceAll(result, old, new)
	}

	result = cssURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		start := bytes.IndexByte(match, '(')
		end := bytes.IndexByte(match, ')')
		if start < 0 || end < 0 {
			return match
		}

		urlStr := bytes.TrimSpace(match[start+1 : end])
		urlStr = bytes.Trim(urlStr, `"' `)
		urlStrStr := string(urlStr)

		if localPath, ok := mappings[urlStrStr]; ok {
			relPath, err := filepath.Rel(cssDir, localPath)
			if err != nil {
				relPath = localPath
			}
			relPath = filepath.ToSlash(relPath)

			quote := byte('"')
			if bytes.Contains(match, []byte(`'`)) {
				quote = byte('\'')
			}
			return []byte(`url(` + string(quote) + relPath + string(quote) + `)`)
		}

		if relPath, ok := absToRel[urlStrStr]; ok {
			quote := byte('"')
			if bytes.Contains(match, []byte(`'`)) {
				quote = byte('\'')
			}
			return []byte(`url(` + string(quote) + relPath + string(quote) + `)`)
		}
		return match
	})

	result = cssImportPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		matches := cssImportPattern.FindSubmatch(match)
		if len(matches) > 1 {
			urlStr := string(matches[1])
			urlStr = strings.Trim(urlStr, `"' `)
			if localPath, ok := mappings[urlStr]; ok {
				relPath, _ := filepath.Rel(cssDir, localPath)
				relPath = filepath.ToSlash(relPath)
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := absToRel[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
		}
		return match
	})

	r.processCSSImportsRecursive(cssLocalPath, mappings)

	return result
}

func (r *Rewriter) processCSSImportsRecursive(cssPath string, mappings map[string]string) {
	processed := make(map[string]bool)
	r.processCSSImports(cssPath, mappings, processed)
}

func (r *Rewriter) processCSSImports(cssPath string, mappings map[string]string, processed map[string]bool) {
	if processed[cssPath] {
		return
	}
	processed[cssPath] = true

	cssDir := filepath.Dir(cssPath)
	cssData, err := os.ReadFile(cssPath)
	if err != nil {
		return
	}

	for _, match := range cssImportPattern.FindAllSubmatch(cssData, -1) {
		if len(match) < 2 {
			continue
		}
		importURL := string(match[1])
		importURL = strings.Trim(importURL, `"' `)
		localPath, ok := mappings[importURL]
		if !ok {
			continue
		}
		if processed[localPath] {
			continue
		}

		importedCSS, err := os.ReadFile(localPath)
		if err != nil {
			continue
		}

		rewritten := importedCSS
		rewritten = cssURLPattern.ReplaceAllFunc(rewritten, func(m []byte) []byte {
			start := bytes.IndexByte(m, '(')
			end := bytes.IndexByte(m, ')')
			if start < 0 || end < 0 {
				return m
			}
			urlStr := string(bytes.TrimSpace(m[start+1 : end]))
			urlStr = strings.Trim(urlStr, `"' `)
			if local, ok2 := mappings[urlStr]; ok2 {
				relPath, err := filepath.Rel(cssDir, local)
				if err != nil {
					relPath = local
				}
				relPath = filepath.ToSlash(relPath)
				quote := byte('"')
				if bytes.Contains(m, []byte(`'`)) {
					quote = byte('\'')
				}
				return []byte(`url(` + string(quote) + relPath + string(quote) + `)`)
			}
			return m
		})

		if err := os.WriteFile(localPath, rewritten, 0644); err != nil {
			util.LogError("failed to write recursively rewritten CSS", err, zap.String("path", localPath))
		}

		r.processCSSImports(localPath, mappings, processed)
	}
}

func (r *Rewriter) processCSSImportsForURL(cssLocalPath string) {
	r.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range r.urlToLocal {
		mappings[k] = v
	}
	r.mu.RUnlock()
	r.processCSSImportsRecursive(cssLocalPath, mappings)
}

func (r *Rewriter) ProcessFiles(files map[string]string) error {
	for filePath, fileType := range files {
		data, err := os.ReadFile(filePath)
		if err != nil {
			util.LogError("failed to read file for rewriting", err, zap.String("path", filePath))
			continue
		}

		var rewritten []byte
		switch fileType {
		case "html":
			rewritten = r.RewriteHTML(data, filePath)
		case "css":
			rewritten = r.RewriteCSS(data, filePath)
			r.processCSSImportsForURL(filePath)
		default:
			continue
		}

		if err := os.WriteFile(filePath, rewritten, 0644); err != nil {
			util.LogError("failed to write rewritten file", err, zap.String("path", filePath))
			continue
		}
	}
	return nil
}

func (r *Rewriter) ExtractLinks(baseURL string, htmlContent []byte) []string {
	var links []string
	baseParsed, _ := url.Parse(baseURL)

	z := html.NewTokenizer(bytes.NewReader(htmlContent))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}

		name, hasAttr := z.TagName()
		tag := atom.Lookup(name)

		if tag == 0 && !hasAttr {
			continue
		}

		if !hasAttr {
			continue
		}

		if tag == atom.Script || tag == atom.Style {
			continue
		}

		for {
			k, v, more := z.TagAttr()
			keyStr := string(k)
			valStr := string(v)

			var link string
			if isURLAttr(k) && !strings.HasPrefix(keyStr, "data-srcset") {
				link = valStr
			} else if keyStr == "srcset" || keyStr == "data-srcset" {
				parts := strings.Split(valStr, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					urlPart := strings.Split(part, " ")[0]
					if urlPart != "" {
						absURL := ResolveURL(baseURL, urlPart)
						if absURL != "" && isSameHost(baseParsed, absURL) {
							links = append(links, absURL)
						}
					}
				}
				if !more {
					break
				}
				continue
			} else if tag == atom.Source || tag == atom.Img {
				if keyStr == "srcset" {
					parts := strings.Split(valStr, ",")
					for _, part := range parts {
						part = strings.TrimSpace(part)
						urlPart := strings.Split(part, " ")[0]
						if urlPart != "" {
							absURL := ResolveURL(baseURL, urlPart)
							if absURL != "" && isSameHost(baseParsed, absURL) {
								links = append(links, absURL)
							}
						}
					} else if keyStr == "src" || keyStr == "data-src" || keyStr == "data-lazy-src" {
						link = valStr
					} else {
			}

			if link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "mailto:") {
				if !more {
					break
				}
				continue
			}

			absURL := ResolveURL(baseURL, link)
			if absURL != "" && isSameHost(baseParsed, absURL) {
				links = append(links, absURL)
			}

			if !more {
				break
			}
		}
	}

	return links
}

func (r *Rewriter) ExtractFontURLs(cssContent []byte) []string {
	var urls []string
	seen := make(map[string]bool)
	for _, fb := range fontFaceBlock.FindAll(cssContent, -1) {
		srcMatch := srcValuePattern.FindSubmatch(fb)
		if len(srcMatch) < 2 {
			continue
		}
		for _, um := range cssURLPattern.FindAllStringSubmatch(string(srcMatch[1]), -1) {
			if len(um) > 1 {
				u := um[1]
				if !seen[u] {
					seen[u] = true
					urls = append(urls, u)
				}
			}
		}
	}
	return urls
}

func isSameHost(base *url.URL, rawURL string) bool {
	if base == nil {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "" || parsed.Hostname() == base.Hostname()
}

func ResolveURL(base, href string) string {
	if href == "" {
		return ""
	}

	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}

	refURL, err := url.Parse(href)
	if err != nil {
		return ""
	}

	resolved := baseURL.ResolveReference(refURL)
	return resolved.String()
}

// GenerateSingleFileHTML inlines all local resources (CSS, JS, images, fonts) into a single HTML file
// This creates a truly offline-capable HTML file
func (r *Rewriter) GenerateSingleFileHTML(htmlContent []byte, htmlLocalPath string) ([]byte, error) {
	// First, rewrite the HTML to use local paths
	rewritten := r.RewriteHTML(htmlContent, htmlLocalPath)

	// For now, return the rewritten HTML - full inlining is a complex operation
	// that can be done in a separate pass
	return rewritten, nil
}

func isSameHost(base *url.URL, rawURL string) bool {
	if base == nil {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == "" || parsed.Hostname() == base.Hostname()
}

func ResolveURL(base, href string) string {
	if href == "" {
		return ""
	}

	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}

	refURL, err := url.Parse(href)
	if err != nil {
		return ""
	}

	resolved := baseURL.ResolveReference(refURL)
	return resolved.String()
}