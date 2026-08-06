package rewrite

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ExtractLinks returns the same-host absolute URLs referenced by the HTML content.
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

		var allAttrs []struct{ k, v []byte }
		for {
			k, v, more := z.TagAttr()
			allAttrs = append(allAttrs, struct{ k, v []byte }{k, v})
			if !more {
				break
			}
		}

		for _, attr := range allAttrs {
			keyStr := string(attr.k)
			valStr := string(attr.v)

			var link string
			if isURLAttr(attr.k) && !strings.HasPrefix(keyStr, "data-srcset") {
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
					}
				} else if keyStr == "src" || keyStr == "data-src" || keyStr == "data-lazy-src" {
					link = valStr
				}
			}

			if link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "mailto:") {
				continue
			}

			absURL := ResolveURL(baseURL, link)
			if absURL != "" && isSameHost(baseParsed, absURL) {
				links = append(links, absURL)
			}
		}
	}

	return links
}

// ExtractFontURLs returns the unique URLs referenced by @font-face src declarations.
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

// ExtractAllCSSURLs returns the unique URLs referenced in the CSS content, excluding data URIs.
func (r *Rewriter) ExtractAllCSSURLs(cssContent []byte) []string {
	var urls []string
	seen := make(map[string]bool)
	for _, um := range cssURLPattern.FindAllStringSubmatch(string(cssContent), -1) {
		if len(um) > 1 {
			u := um[1]
			if !seen[u] && !strings.HasPrefix(u, "data:") {
				seen[u] = true
				urls = append(urls, u)
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

// ResolveURL resolves href against base, returning it unchanged if already absolute.
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
