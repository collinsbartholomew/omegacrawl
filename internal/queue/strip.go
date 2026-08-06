package queue

import (
	"net/url"
	"strings"
)

// NormalizeURL lowercases the scheme and host, collapses and resolves the path,
// strips default ports and tracking query parameters, and sorts remaining params.
func NormalizeURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return rawURL
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return rawURL
	}

	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	var hostStr string
	if port != "" {
		hostStr = host + ":" + port
	} else {
		hostStr = host
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	path = normalizePath(path)

	cleanURL := scheme + "://" + hostStr + path

	if u.RawQuery != "" {
		sortedQuery := sortQueryParams(u.RawQuery)
		if sortedQuery != "" {
			cleanURL += "?" + sortedQuery
		}
	}

	return cleanURL
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}

	path = strings.ReplaceAll(path, "/./", "/")

	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	segments := strings.Split(path, "/")
	var resolved []string
	for _, seg := range segments {
		if seg == ".." {
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
		} else if seg != "." {
			resolved = append(resolved, seg)
		}
	}

	path = strings.Join(resolved, "/")

	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	if path == "" {
		path = "/"
	}

	path = strings.ReplaceAll(path, "%7E", "~")

	return path
}

func sortQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	params, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}

	for k := range params {
		if shouldStripParam(k) {
			delete(params, k)
		}
	}

	if len(params) == 0 {
		return ""
	}

	return params.Encode()
}

// NormalizeAndClean normalizes the URL and preserves fragment-style URLs.
func NormalizeAndClean(rawURL string) string {
	u, err := url.Parse(rawURL)
	var fragment string
	if err == nil && u.Fragment != "" && (strings.HasPrefix(u.Fragment, "/") || strings.HasPrefix(u.Fragment, "!/")) {
		fragment = "#" + u.Fragment
	}

	normalized := NormalizeURL(rawURL)

	if fragment != "" && !strings.HasSuffix(normalized, fragment) {
		return normalized + fragment
	}
	return normalized
}
