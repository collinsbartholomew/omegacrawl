package localize

import (
	"regexp"
	"strings"
)

// cssImportQuotedPattern matches a bare quoted @import like
// `@import "theme.css";` or `@import 'theme.css' layer(foo)`. The url(...)
// form is handled by the generic url() scanner.
var cssImportQuotedPattern = regexp.MustCompile(`(?i)@import\s+("([^"]+)"|'([^']+)')`)

// rewriteCSSRefs rewrites url(...) and @import references in CSS text.
func (r *Rewriter) rewriteCSSRefs(css, baseURL, dir string) string {
	css = r.rewriteCSSImports(css, baseURL, dir)

	var sb strings.Builder
	lower := strings.ToLower(css)
	i := 0
	for {
		idx := strings.Index(lower[i:], "url(")
		if idx < 0 {
			sb.WriteString(css[i:])
			break
		}
		idx += i
		sb.WriteString(css[i:idx])

		rest := css[idx+4:]
		j := 0
		depth := 1
		for j < len(rest) && depth > 0 {
			ch := rest[j]
			if ch == '\\' && j+1 < len(rest) {
				j += 2
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
			j++
		}
		content := rest[:j]
		urlStr := strings.TrimSpace(content)
		urlStr = strings.Trim(urlStr, `"' `)

		if urlStr != "" && !strings.HasPrefix(urlStr, "data:") {
			if rel, ok := r.localize(urlStr, baseURL, dir); ok {
				sb.WriteString(`url("` + rel + `")`)
				i = idx + 4 + j + 1
				continue
			}
		}
		sb.WriteString("url(")
		sb.WriteString(content)
		sb.WriteString(")")
		i = idx + 4 + j + 1
	}
	return sb.String()
}

// rewriteCSSImports localizes bare quoted @import references, leaving any that
// cannot be resolved untouched.
func (r *Rewriter) rewriteCSSImports(css, baseURL, dir string) string {
	return cssImportQuotedPattern.ReplaceAllStringFunc(css, func(m string) string {
		sub := cssImportQuotedPattern.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		quote := `"`
		ref := strings.TrimSpace(sub[2])
		if ref == "" {
			quote = "'"
			ref = strings.TrimSpace(sub[3])
		}
		if ref == "" || strings.HasPrefix(ref, "data:") {
			return m
		}
		if rel, ok := r.localize(ref, baseURL, dir); ok {
			return "@import " + quote + rel + quote
		}
		return m
	})
}
