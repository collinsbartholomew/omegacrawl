package jsanalyzer

import (
	"regexp"
	"strings"
)

// AnalyzedURL pairs an extracted URL with the pattern type that produced it.
type AnalyzedURL struct {
	URL  string
	Type string
}

var (
	dynamicImportRegex  = regexp.MustCompile(`import\s*\(\s*["']([^"']+)["']\s*\)`)
	requireRegex        = regexp.MustCompile(`require\s*\(\s*["']([^"']+)["']\s*\)`)
	fetchRegex          = regexp.MustCompile(`fetch\s*\(\s*["']([^"']+)["']`)
	xhrOpenRegex        = regexp.MustCompile(`\.open\s*\(\s*["'][^"']*["']\s*,\s*["']([^"']+)["']`)
	importScriptsRegex  = regexp.MustCompile(`importScripts\s*\(\s*["']([^"']+)["']`)
	systemImportRegex   = regexp.MustCompile(`System\.import\s*\(\s*["']([^"']+)["']`)
	axiosRegex          = regexp.MustCompile(`axios\s*\.\s*(?:get|post|put|patch|delete|head|options)\s*\(\s*["']([^"']+)["']`)
	jqueryAjaxRegex     = regexp.MustCompile(`\$\.ajax\s*\(\s*\{[^}]*url\s*:\s*["']([^"']+)["']`)
	defineAsyncRegex    = regexp.MustCompile(`defineAsyncComponent\s*\(\s*\(\s*\)\s*=>\s*import\s*\(\s*["']([^"']+)["']`)
	reactLazyRegex      = regexp.MustCompile(`React\.lazy\s*\(\s*\(\s*\)\s*=>\s*import\s*\(\s*["']([^"']+)["']`)
	vueLazyRegex        = regexp.MustCompile(`Vue\.component\s*\(\s*["'][^"']+["']\s*,\s*\(\s*\)\s*=>\s*import\s*\(\s*["']([^"']+)["']`)
	webpackChunkRegex   = regexp.MustCompile(`webpackChunkName\s*:\s*["']([^"']+)["']`)
	amazonRequireRegex  = regexp.MustCompile(`__webpack_require__\s*\(\s*["']([^"']+)["']`)
	importMapRegex      = regexp.MustCompile(`<script[^>]*type=["']importmap["'][^>]*>([\s\S]*?)</script>`)
	moduleScriptRegex   = regexp.MustCompile(`<script[^>]*type=["']module["'][^>]*src=["']([^"']+)["']`)
	urlInImportMapRegex = regexp.MustCompile(`["']([^"']+)["']\s*:\s*["']([^"']+)["']`)
)

// ExtractJSURLs scans JavaScript content for resource URLs and returns deduplicated results resolved against baseURL.
func ExtractJSURLs(jsContent, baseURL string) []AnalyzedURL {
	var urls []AnalyzedURL
	seen := make(map[string]bool)

	for _, u := range extractWithRegex(jsContent, baseURL) {
		key := u.URL + "|" + u.Type
		if !seen[key] {
			seen[key] = true
			urls = append(urls, u)
		}
	}

	return urls
}

func extractWithRegex(jsContent, baseURL string) []AnalyzedURL {
	var urls []AnalyzedURL

	patterns := map[string]*regexp.Regexp{
		"dynamic-import":  dynamicImportRegex,
		"require":         requireRegex,
		"fetch":           fetchRegex,
		"xhr-open":        xhrOpenRegex,
		"importScripts":   importScriptsRegex,
		"system-import":   systemImportRegex,
		"axios":           axiosRegex,
		"jquery-ajax":     jqueryAjaxRegex,
		"define-async":    defineAsyncRegex,
		"react-lazy":      reactLazyRegex,
		"vue-lazy":        vueLazyRegex,
		"webpack-chunk":   webpackChunkRegex,
		"webpack-require": amazonRequireRegex,
	}

	for urlType, re := range patterns {
		matches := re.FindAllStringSubmatch(jsContent, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				url := resolveURL(baseURL, match[1])
				if isValidURL(url) {
					urls = append(urls, AnalyzedURL{URL: url, Type: urlType})
				}
			}
		}
	}

	return urls
}

// ExtractFromHTML scans HTML content for import maps and module scripts, returning deduplicated URLs resolved against baseURL.
func ExtractFromHTML(htmlContent, baseURL string) []AnalyzedURL {
	var urls []AnalyzedURL
	seen := make(map[string]bool)

	matches := importMapRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			mapURLs := extractImportMapURLs(match[1], baseURL)
			for _, u := range mapURLs {
				key := u.URL + "|" + u.Type
				if !seen[key] {
					seen[key] = true
					urls = append(urls, u)
				}
			}
		}
	}

	matches = moduleScriptRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			url := resolveURL(baseURL, match[1])
			if isValidURL(url) {
				key := url + "|module-script"
				if !seen[key] {
					seen[key] = true
					urls = append(urls, AnalyzedURL{URL: url, Type: "module-script"})
				}
			}
		}
	}

	return urls
}

func extractImportMapURLs(importMapContent, baseURL string) []AnalyzedURL {
	var urls []AnalyzedURL
	matches := urlInImportMapRegex.FindAllStringSubmatch(importMapContent, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			url := resolveURL(baseURL, match[2])
			if isValidURL(url) {
				urls = append(urls, AnalyzedURL{URL: url, Type: "importmap"})
			}
		}
	}
	return urls
}

func resolveURL(baseURL, ref string) string {
	if ref == "" {
		return ""
	}

	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}

	if strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "blob:") {
		return ""
	}

	base, err := parseURL(baseURL)
	if err != nil {
		return ""
	}

	refURL, err := parseURL(ref)
	if err != nil {
		return ""
	}

	return base.ResolveReference(refURL).String()
}

func parseURL(raw string) (*URL, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		protoEnd := strings.Index(raw, "://")
		rest := raw[protoEnd+3:]
		hostEnd := strings.Index(rest, "/")
		if hostEnd == -1 {
			return &URL{Scheme: raw[:protoEnd], Host: rest, Path: "/"}, nil
		}
		return &URL{
			Scheme: raw[:protoEnd],
			Host:   rest[:hostEnd],
			Path:   rest[hostEnd:],
		}, nil
	}
	return &URL{Path: raw}, nil
}

// URL is a minimal URL representation used for reference resolution.
type URL struct {
	Scheme string
	Host   string
	Path   string
}

// ResolveReference resolves ref against the receiver URL, returning ref unchanged when it is absolute.
func (u *URL) ResolveReference(ref *URL) *URL {
	if ref.Scheme != "" {
		return ref
	}
	if ref.Host != "" {
		return &URL{Scheme: u.Scheme, Host: ref.Host, Path: ref.Path}
	}
	return &URL{Scheme: u.Scheme, Host: u.Host, Path: resolvePath(u.Path, ref.Path)}
}

func resolvePath(basePath, refPath string) string {
	if strings.HasPrefix(refPath, "/") {
		return refPath
	}

	baseDir := basePath
	lastSlash := strings.LastIndex(baseDir, "/")
	if lastSlash != -1 {
		baseDir = baseDir[:lastSlash+1]
	} else {
		baseDir = "/"
	}

	parts := strings.Split(baseDir+refPath, "/")
	var result []string
	for _, p := range parts {
		if p == ".." {
			if len(result) > 0 {
				result = result[:len(result)-1]
			}
		} else if p != "" && p != "." {
			result = append(result, p)
		}
	}

	return "/" + strings.Join(result, "/")
}

// String returns the URL as a string, using the path alone when no scheme or host is set.
func (u *URL) String() string {
	if u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + u.Path
	}
	return u.Path
}

func isValidURL(url string) bool {
	if url == "" {
		return false
	}
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}
