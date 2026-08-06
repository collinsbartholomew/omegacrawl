package localize

import (
	"net/url"
	"path/filepath"
	"strings"
)

func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

// resolveURL resolves ref against base, returning ref unchanged if already
// absolute. An empty base yields an empty result for relative refs.
func resolveURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if base == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	rr, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return b.ResolveReference(rr).String()
}
