package localize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// JSReWriter handles JavaScript bundle localization for webpack/Next.js/Rspack/Turbopack
type JSReWriter struct {
	urlToLocal  map[string]string
	pageURLs    map[string]string
	localRoot   string
	assetPrefix string
	buildID     string
	nextData    map[string]any

	// Compiled regex patterns for performance
	publicPathRegex       *regexp.Regexp
	dynamicImportRegex    *regexp.Regexp
	nextDataRegex         *regexp.Regexp
	nextImageLoaderRegex  *regexp.Regexp
	webpackChunkLoadRegex *regexp.Regexp
	assetPrefixRegex      *regexp.Regexp
	buildIDRegex          *regexp.Regexp
	urlInStringRegex      *regexp.Regexp
}

// NewJSReWriter creates a new JavaScript bundle rewriter
func NewJSReWriter(urlToLocal, pageURLs map[string]string, localRoot string) *JSReWriter {
	rw := &JSReWriter{
		urlToLocal: urlToLocal,
		pageURLs:   pageURLs,
		localRoot:  localRoot,
	}
	rw.compilePatterns()
	return rw
}

func (rw *JSReWriter) compilePatterns() {
	// Match __webpack_require__.p = "..." or __webpack_require__.p='...'
	rw.publicPathRegex = regexp.MustCompile(`__webpack_require__\.p\s*=\s*["']([^"']+)["']`)

	// Match dynamic import() calls: import("...") or import('...') or import(`...`)
	// Using regular string to avoid backtick in raw string
	rw.dynamicImportRegex = regexp.MustCompile("import\\s*\\(\\s*[\"'`]+([^\"'`]+)[\"'`]+\\s*\\)")

	// Match __NEXT_DATA__ = {...} in HTML
	rw.nextDataRegex = regexp.MustCompile(`<script[^>]*id=["']__NEXT_DATA__["'][^>]*>\s*(\{.*?\})\s*</script>`)

	// Match next/image loader configuration
	rw.nextImageLoaderRegex = regexp.MustCompile(`__next_image_loader__\s*=\s*["']([^"']+)["']`)

	// Match webpack chunk loading: __webpack_require__.e/* ... */("chunkName")
	rw.webpackChunkLoadRegex = regexp.MustCompile(`__webpack_require__\.e\s*\(\s*["']([^"']+)["']\s*\)`)

	// Match __webpack_require__.p assignments in various forms
	rw.assetPrefixRegex = regexp.MustCompile(`(["'])assetPrefix\1\s*:\s*\1([^"']+)\1`)
	rw.buildIDRegex = regexp.MustCompile(`(["'])buildId\1\s*:\s*\1([^"']+)\1`)

	// Match URLs in JS strings - using regular string to avoid backtick in raw string
	rw.urlInStringRegex = regexp.MustCompile("['\"`]+(https?://[^'\"`\\s]+)['\"`]+")
}

// RewriteJS localizes a JavaScript bundle file
func (rw *JSReWriter) RewriteJS(data []byte, jsPath string) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}

	original := string(data)
	modified := original
	changed := false

	// 1. Rewrite webpack public path (__webpack_require__.p)
	if newContent, ok := rw.rewritePublicPath(modified); ok {
		modified = newContent
		changed = true
	}

	// 2. Rewrite dynamic imports
	if newContent, ok := rw.rewriteDynamicImports(modified); ok {
		modified = newContent
		changed = true
	}

	// 3. Rewrite webpack chunk loading
	if newContent, ok := rw.rewriteChunkLoading(modified); ok {
		modified = newContent
		changed = true
	}

	// 4. Rewrite Next.js specific patterns (assetPrefix, buildId in runtime)
	if newContent, ok := rw.rewriteNextJSRuntime(modified); ok {
		modified = newContent
		changed = true
	}

	// 5. Rewrite next/image loader URLs
	if newContent, ok := rw.rewriteNextImageLoader(modified); ok {
		modified = newContent
		changed = true
	}

	// 6. Rewrite any remaining absolute URLs in JS strings
	if newContent, ok := rw.rewriteJSStringURLs(modified, jsPath); ok {
		modified = newContent
		changed = true
	}

	return []byte(modified), changed
}

// rewritePublicPath rewrites __webpack_require__.p to use relative path
func (rw *JSReWriter) rewritePublicPath(content string) (string, bool) {
	matches := rw.publicPathRegex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	var buf bytes.Buffer
	buf.Grow(len(content))
	lastEnd := 0

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		urlStart, urlEnd := match[2], match[3]

		buf.WriteString(content[lastEnd:fullStart])

		quote := content[urlStart : urlStart+1]
		originalURL := content[urlStart+1 : urlEnd-1]

		newURL := rw.computeRelativePublicPath(originalURL, quote)

		buf.WriteString(content[fullStart : urlStart+1])
		buf.WriteString(newURL)
		buf.WriteString(quote)

		lastEnd = fullEnd
	}

	buf.WriteString(content[lastEnd:])
	return buf.String(), true
}

// computeRelativePublicPath converts an absolute public path to a relative one
func (rw *JSReWriter) computeRelativePublicPath(originalURL, quote string) string {
	// If already relative, keep as-is
	if !strings.HasPrefix(originalURL, "http://") && !strings.HasPrefix(originalURL, "https://") && !strings.HasPrefix(originalURL, "//") {
		return originalURL
	}

	// For localized copy, use relative path to _next directory
	return "./_next/"
}

// rewriteDynamicImports rewrites import("...") calls to use local paths
func (rw *JSReWriter) rewriteDynamicImports(content string) (string, bool) {
	matches := rw.dynamicImportRegex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	var buf bytes.Buffer
	buf.Grow(len(content))
	lastEnd := 0
	changed := false

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		urlStart, urlEnd := match[2], match[3]

		buf.WriteString(content[lastEnd:fullStart])

		quote := content[urlStart : urlStart+1]
		originalURL := content[urlStart+1 : urlEnd-1]

		newURL := rw.resolveDynamicImport(originalURL)

		buf.WriteString(content[fullStart : urlStart+1])
		buf.WriteString(newURL)
		buf.WriteString(quote)

		lastEnd = fullEnd
		changed = true
	}

	buf.WriteString(content[lastEnd:])
	return buf.String(), changed
}

// resolveDynamicImport converts a dynamic import URL to local path
func (rw *JSReWriter) resolveDynamicImport(originalURL string) string {
	// Handle relative imports
	if strings.HasPrefix(originalURL, "./") || strings.HasPrefix(originalURL, "../") {
		return originalURL
	}

	// Handle absolute URLs - try to map to local
	if localPath, ok := rw.urlToLocal[originalURL]; ok {
		return localPath
	}

	// Try with normalized URL
	if u, err := parseURL(originalURL); err == nil {
		normalized := u.Scheme + "://" + u.Host + u.Path
		if localPath, ok := rw.urlToLocal[normalized]; ok {
			return localPath
		}
	}

	return originalURL
}

// rewriteChunkLoading rewrites webpack chunk loading calls
func (rw *JSReWriter) rewriteChunkLoading(content string) (string, bool) {
	matches := rw.webpackChunkLoadRegex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	var buf bytes.Buffer
	buf.Grow(len(content))
	lastEnd := 0
	changed := false

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		urlStart, urlEnd := match[2], match[3]

		buf.WriteString(content[lastEnd:fullStart])

		quote := content[urlStart : urlStart+1]
		chunkName := content[urlStart+1 : urlEnd-1]

		buf.WriteString(content[fullStart : urlStart+1])
		buf.WriteString(chunkName)
		buf.WriteString(quote)

		lastEnd = fullEnd
		changed = true
	}

	buf.WriteString(content[lastEnd:])
	return buf.String(), changed
}

// rewriteNextJSRuntime rewrites Next.js runtime configuration (assetPrefix, buildId)
func (rw *JSReWriter) rewriteNextJSRuntime(content string) (string, bool) {
	changed := false

	// Rewrite assetPrefix in runtime config
	assetPrefixMatches := rw.assetPrefixRegex.FindAllStringSubmatchIndex(content, -1)
	if len(assetPrefixMatches) > 0 {
		var buf bytes.Buffer
		buf.Grow(len(content))
		lastEnd := 0

		for _, match := range assetPrefixMatches {
			fullStart, fullEnd := match[0], match[1]
			urlStart, urlEnd := match[2], match[3]

			buf.WriteString(content[lastEnd:fullStart])
			buf.WriteString(content[fullStart : urlStart+1])
			buf.WriteString("./") // Relative asset prefix
			buf.WriteString(content[urlEnd-1 : fullEnd])
			lastEnd = fullEnd
			changed = true
		}

		buf.WriteString(content[lastEnd:])
		content = buf.String()
	}

	// Rewrite buildId
	buildIDMatches := rw.buildIDRegex.FindAllStringSubmatchIndex(content, -1)
	if len(buildIDMatches) > 0 {
		var buf bytes.Buffer
		buf.Grow(len(content))
		lastEnd := 0

		for _, match := range buildIDMatches {
			fullStart, fullEnd := match[0], match[1]
			urlStart, urlEnd := match[2], match[3]

			buf.WriteString(content[lastEnd:fullStart])
			buf.WriteString(content[fullStart : urlStart+1])
			// Keep original buildId for cache busting
			buf.WriteString(content[urlStart:urlEnd])
			buf.WriteString(content[urlEnd:fullEnd])
			lastEnd = fullEnd
		}

		buf.WriteString(content[lastEnd:])
		content = buf.String()
	}

	return content, changed
}

// rewriteNextImageLoader rewrites next/image loader URLs
func (rw *JSReWriter) rewriteNextImageLoader(content string) (string, bool) {
	matches := rw.nextImageLoaderRegex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	var buf bytes.Buffer
	buf.Grow(len(content))
	lastEnd := 0
	changed := false

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		urlStart, urlEnd := match[2], match[3]

		buf.WriteString(content[lastEnd:fullStart])

		quote := content[urlStart : urlStart+1]
		loaderURL := content[urlStart+1 : urlEnd-1]

		newLoader := rw.resolveDynamicImport(loaderURL)

		buf.WriteString(content[fullStart : urlStart+1])
		buf.WriteString(newLoader)
		buf.WriteString(quote)

		lastEnd = fullEnd
		changed = true
	}

	buf.WriteString(content[lastEnd:])
	return buf.String(), changed
}

// rewriteJSStringURLs rewrites any remaining absolute URLs in JS string literals
func (rw *JSReWriter) rewriteJSStringURLs(content, jsPath string) (string, bool) {
	matches := rw.urlInStringRegex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}

	var buf bytes.Buffer
	buf.Grow(len(content))
	lastEnd := 0
	changed := false

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		urlStart, urlEnd := match[2], match[3]

		urlContent := content[urlStart:urlEnd]
		if strings.HasPrefix(urlContent, "./") ||
			strings.HasPrefix(urlContent, "../") ||
			strings.HasPrefix(urlContent, "data:") ||
			strings.HasPrefix(urlContent, "blob:") ||
			strings.HasPrefix(urlContent, "javascript:") ||
			strings.HasPrefix(urlContent, "mailto:") ||
			strings.HasPrefix(urlContent, "tel:") {
			continue
		}

		if localPath, ok := rw.urlToLocal[urlContent]; ok {
			buf.WriteString(content[lastEnd:fullStart])

			quote := content[fullStart : fullStart+1]
			relPath := rw.computeRelativePath(jsPath, localPath)

			buf.WriteString(quote)
			buf.WriteString(relPath)
			buf.WriteString(quote)

			lastEnd = fullEnd
			changed = true
		}
	}

	buf.WriteString(content[lastEnd:])
	return buf.String(), changed
}

// computeRelativePath computes relative path from source file to target file
func (rw *JSReWriter) computeRelativePath(sourcePath, targetPath string) string {
	sourceRel, err1 := filepath.Rel(rw.localRoot, sourcePath)
	targetRel, err2 := filepath.Rel(rw.localRoot, targetPath)
	if err1 != nil || err2 != nil {
		return targetPath
	}

	sourceDir := filepath.Dir(sourceRel)
	rel, err := filepath.Rel(sourceDir, targetRel)
	if err != nil {
		return targetRel
	}

	return filepath.ToSlash(rel)
}

// RewriteNextDataInHTML rewrites __NEXT_DATA__ script tag in HTML
func (rw *JSReWriter) RewriteNextDataInHTML(htmlContent []byte) ([]byte, bool) {
	matches := rw.nextDataRegex.FindAllSubmatchIndex(htmlContent, -1)
	if len(matches) == 0 {
		return htmlContent, false
	}

	var buf bytes.Buffer
	buf.Grow(len(htmlContent))
	lastEnd := 0
	changed := false

	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		jsonStart, jsonEnd := match[2], match[3]

		buf.Write(htmlContent[lastEnd:fullStart])

		jsonBytes := htmlContent[jsonStart:jsonEnd]
		var nextData map[string]any
		if err := json.Unmarshal(jsonBytes, &nextData); err == nil {
			rw.modifyNextData(nextData)

			newJSON, err := json.Marshal(nextData)
			if err == nil {
				buf.Write(htmlContent[fullStart:jsonStart])
				buf.Write(newJSON)
				buf.Write(htmlContent[jsonEnd:fullEnd])
				changed = true
			} else {
				buf.Write(htmlContent[fullStart:fullEnd])
			}
		} else {
			buf.Write(htmlContent[fullStart:fullEnd])
		}

		lastEnd = fullEnd
	}

	buf.Write(htmlContent[lastEnd:])
	return buf.Bytes(), changed
}

// modifyNextData modifies __NEXT_DATA__ for localization
func (rw *JSReWriter) modifyNextData(data map[string]any) {
	// Set assetPrefix to empty for relative paths
	data["assetPrefix"] = ""

	// Modify dynamicImports if present
	if dynImports, ok := data["dynamicImports"].([]any); ok {
		for i, imp := range dynImports {
			if impStr, ok := imp.(string); ok {
				if localPath, ok := rw.urlToLocal[impStr]; ok {
					dynImports[i] = localPath
				}
			}
		}
	}

	// Store for potential use in JS rewriting
	rw.nextData = data
}

// RewriteHTMLWithJS handles both HTML and inline JS rewriting
func (rw *JSReWriter) RewriteHTMLWithJS(htmlContent []byte, htmlPath string) []byte {
	content, _ := rw.RewriteNextDataInHTML(htmlContent)
	return content
}

// Helper function to parse URL
func parseURL(rawURL string) (*ParsedURL, error) {
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd == -1 {
		return nil, fmt.Errorf("invalid URL")
	}
	scheme := rawURL[:schemeEnd]
	rest := rawURL[schemeEnd+3:]

	hostEnd := strings.Index(rest, "/")
	var host, path string
	if hostEnd == -1 {
		host = rest
		path = "/"
	} else {
		host = rest[:hostEnd]
		path = rest[hostEnd:]
	}

	return &ParsedURL{Scheme: scheme, Host: host, Path: path}, nil
}

type ParsedURL struct {
	Scheme string
	Host   string
	Path   string
}

func (u *ParsedURL) Hostname() string {
	if idx := strings.Index(u.Host, ":"); idx != -1 {
		return u.Host[:idx]
	}
	return u.Host
}
