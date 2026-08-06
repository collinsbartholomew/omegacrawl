package repair

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
)

// rewriteRepairedURLs replaces each repaired absolute URL in the page with a
// path relative to the page directory, handling CSS backslash-escaped forms.
// Mappings are applied longest-URL-first so a longer URL that shares a prefix
// with a shorter one (e.g. /a vs /a/b) is never left half-replaced by an
// earlier shorter match.
func rewriteRepairedURLs(pageHTML []byte, pagePath string, mapping map[string]string) []byte {
	htmlDir := filepath.Dir(pagePath)

	urls := make([]string, 0, len(mapping))
	for u := range mapping {
		urls = append(urls, u)
	}
	sort.Slice(urls, func(i, j int) bool {
		return len(urls[i]) > len(urls[j])
	})

	for _, u := range urls {
		p := mapping[u]
		rel := filepath.ToSlash(filepath.Clean(relPath(htmlDir, p)))
		pageHTML = bytes.ReplaceAll(pageHTML, []byte(u), []byte(rel))
		escU := cssEscapeURL(u)
		escRel := cssEscapeURL(rel)
		if escU != u {
			pageHTML = bytes.ReplaceAll(pageHTML, []byte(escU), []byte(escRel))
		}
	}
	return pageHTML
}

func relPath(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return r
}

// cssEscapeURL backslash-escapes the characters that must be escaped inside a
// CSS url(...) token.
func cssEscapeURL(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(', ')', ',', '\'', '"', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

type pageRef struct {
	path string
	url  string
}
