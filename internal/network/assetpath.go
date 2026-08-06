package network

import (
	"net/url"
	"path"
	"strings"
)

// AssetPath maps a URL and MIME type to a local filesystem path for the asset.
func AssetPath(urlStr string, mimeType string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	ext := path.Ext(u.Path)
	if ext == "" {
		ext = extensionForMime(mimeType)
	}

	host := u.Hostname()
	cleanPath := u.Path
	if cleanPath == "" {
		cleanPath = "/index"
	}

	return path.Join(host, cleanPath)
}

func extensionForMime(mime string) string {
	switch {
	case strings.Contains(mime, "text/html"):
		return ".html"
	case strings.Contains(mime, "text/css"):
		return ".css"
	case strings.Contains(mime, "javascript"):
		return ".js"
	case strings.Contains(mime, "image/png"):
		return ".png"
	case strings.Contains(mime, "image/jpeg"):
		return ".jpg"
	case strings.Contains(mime, "image/gif"):
		return ".gif"
	case strings.Contains(mime, "image/svg"):
		return ".svg"
	case strings.Contains(mime, "image/webp"):
		return ".webp"
	case strings.Contains(mime, "image/x-icon"):
		return ".ico"
	case strings.Contains(mime, "font/woff2"):
		return ".woff2"
	case strings.Contains(mime, "font/woff"):
		return ".woff"
	case strings.Contains(mime, "font/ttf"):
		return ".ttf"
	case strings.Contains(mime, "font/eot"):
		return ".eot"
	case strings.Contains(mime, "application/json"):
		return ".json"
	case strings.Contains(mime, "application/pdf"):
		return ".pdf"
	default:
		return ""
	}
}

func baseMimeType(mime string) string {
	if idx := strings.IndexByte(mime, ';'); idx != -1 {
		return strings.TrimSpace(mime[:idx])
	}
	return strings.TrimSpace(mime)
}

func isJSONContentType(mime string) bool {
	base := baseMimeType(mime)
	switch base {
	case "application/json", "text/json",
		"application/vnd.api+json", "application/problem+json",
		"application/hal+json", "application/ld+json":
		return true
	}
	return false
}

func isAPIContentType(urlStr, mime string) bool {
	base := baseMimeType(mime)
	if strings.Contains(base, "xml") || strings.Contains(base, "protobuf") || strings.Contains(base, "grpc") {
		return true
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	lowPath := strings.ToLower(u.Path)
	if strings.Contains(lowPath, "/api/") || strings.Contains(lowPath, "/graphql") || strings.Contains(lowPath, "/gql") ||
		strings.Contains(lowPath, "/rest/") || strings.Contains(lowPath, "/v1/") || strings.Contains(lowPath, "/v2/") ||
		strings.Contains(lowPath, "/rpc") || strings.Contains(lowPath, "jsonrpc") {
		return true
	}
	return false
}
