package localize

import (
	"net/url"
	"strconv"
	"strings"
)

// queryDropParams lists query keys that are session/commerce/tracking noise,
// never distinct content. They are ignored when grouping duplicate pages.
// Content-affecting keys (orderby, order, term, taxonomy, rating_filter,
// product_cat, product_tag, filter_*) are deliberately NOT dropped because
// they select different documents.
var queryDropParams = []string{
	"paged", "page", "replytocom", "add-to-cart", "added-to-cart",
	"remove_item", "download_item", "wc-ajax", "preview",
	"no_reply", "amp", "consumer_key", "nonce",
	"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
	"gclid", "fbclid", "msclkid", "ref", "source", "session_id", "s_id",
}

var (
	dropParamsSet = func() map[string]bool {
		m := make(map[string]bool, len(queryDropParams))
		for _, k := range queryDropParams {
			m[k] = true
		}
		return m
	}()
	dropPrefixes = []string{"utm_"}
	// pathDropSegmentSuffix are path segments that mark pagination. A bare
	// numeric segment following one of these is also treated as pagination.
	pathDropSegmentSuffix = []string{"page", "paged", "comment-page"}
)

// noisePathPrefixes are URL paths that never represent real user-facing content
// (REST APIs, cart/checkout/account workflows, feeds, admin). They are dropped
// from a deduplicated export intended for a re-platform migration.
var noisePathPrefixes = []string{
	"/wp-json", "/rest/", "/wp-admin", "/wp-login", "/wp-cron", "/wp-includes/",
	"/xmlrpc", "/feed", "/cart", "/checkout", "/my-account", "/wc-api",
	"/wc-ajax", "/addons", "/robots.txt", "/sitemap", "/wp-sitemap",
	"/oembed/", "/.well-known", "/feed/",
}

// canonicalizer groups page URLs into unique documents. Pagination path
// segments and noise query params are dropped unless explicitly preserved;
// preserved names override the defaults so sites that use them as real content
// are not collapsed.
type canonicalizer struct {
	preserveQueryParams  map[string]bool
	preservePathSegments map[string]bool
}

func newCanonicalizer(preserveQueryParams, preservePathSegments []string) *canonicalizer {
	c := &canonicalizer{
		preserveQueryParams:  map[string]bool{},
		preservePathSegments: map[string]bool{},
	}
	for _, k := range preserveQueryParams {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" {
			c.preserveQueryParams[k] = true
		}
	}
	for _, s := range preservePathSegments {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			c.preservePathSegments[s] = true
		}
	}
	return c
}

// canonicalKey returns a grouping key for a reconstructed page URL, dropping
// path segments (pagination) and query params that only duplicate a page.
func (c *canonicalizer) canonicalKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = c.stripDropQuery(u.RawQuery)
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	kept := make([]string, 0, len(segs))
	prev := ""
	for _, s := range segs {
		if c.isDropSegment(s, prev) {
			prev = s
			continue
		}
		kept = append(kept, s)
		prev = s
	}
	u.Path = "/" + strings.Join(kept, "/")
	return u.String()
}

// isNoisePath reports whether a URL path is a non-content noise route.
func isNoisePath(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	lp := strings.ToLower(u.Path)
	for _, n := range noisePathPrefixes {
		if strings.HasPrefix(lp, n) {
			return true
		}
	}
	return false
}

// isDropSegment reports whether a path segment is a pagination marker. Only
// explicit markers ("page", "paged", "comment-page*") and the numeric page
// number that immediately follows such a marker are dropped. Bare numeric
// segments are kept so dated URLs like /news/2026/05/12/slug do not collapse
// into a single representative. A segment (or the marker it follows) that is
// explicitly preserved is always treated as content.
func (c *canonicalizer) isDropSegment(s, prev string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" {
		return false
	}
	pl := strings.ToLower(strings.TrimSpace(prev))
	if c.preservePathSegments[l] || c.preservePathSegments[pl] {
		return false
	}
	if l == "page" || l == "paged" {
		return true
	}
	for _, pref := range pathDropSegmentSuffix {
		if l == pref || strings.HasPrefix(l, pref+"-") || strings.HasPrefix(l, pref+"_") {
			return true
		}
	}
	if _, err := strconv.ParseInt(l, 10, 64); err == nil {
		for _, pref := range pathDropSegmentSuffix {
			if pl == pref || strings.HasPrefix(pl, pref+"-") || strings.HasPrefix(pl, pref+"_") {
				return true
			}
		}
	}
	return false
}

// stripDropQuery removes noise query params, returning the surviving raw query.
func (c *canonicalizer) stripDropQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		k := p
		if i := strings.IndexByte(p, '='); i >= 0 {
			k = p[:i]
		}
		key, _ := url.QueryUnescape(k)
		lk := strings.ToLower(key)
		if c.preserveQueryParams[lk] {
			kept = append(kept, p)
			continue
		}
		if dropParamsSet[lk] {
			continue
		}
		dropped := false
		for _, pf := range dropPrefixes {
			if strings.HasPrefix(lk, pf) {
				dropped = true
				break
			}
		}
		if !dropped {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "&")
}

// cleanWeight scores how "noisy" a page URL is; lower is cleaner. The cleanest
// representative of each duplicate group is kept.
func cleanWeight(rawURL string) int {
	u, _ := url.Parse(rawURL)
	w := 0
	if u.RawQuery != "" {
		w += 10
	}
	if strings.Contains(u.Path, "/page/") || strings.Contains(u.Path, "/comment-page") {
		w += 5
	}
	return w
}
