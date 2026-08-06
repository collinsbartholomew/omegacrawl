package storage

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// PathForURL returns the local filesystem path where content for the given
// URL should be stored, guarding against path traversal.
func (fs *Filesystem) PathForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return filepath.Join(fs.outputDir, "unknown")
	}

	host := u.Hostname()
	if host == "" {
		host = "_unknown"
	}

	cleanPath := u.Path
	if cleanPath == "" || cleanPath == "/" {
		cleanPath = "/index.html"
	}

	decoded, err := url.PathUnescape(cleanPath)
	if err != nil {
		decoded = cleanPath
	}

	if strings.HasSuffix(decoded, "/") {
		decoded += "index.html"
	}

	decoded = strings.TrimPrefix(decoded, "/")

	baseName := decoded
	if idx := strings.LastIndex(decoded, "/"); idx >= 0 {
		baseName = decoded[idx+1:]
	}
	if !strings.Contains(baseName, ".") {
		decoded += "/index.html"
	}

	decoded = filepath.FromSlash(decoded)

	if strings.Contains(decoded, "..") {
		decoded = "_safe_path"
	}

	if u.RawQuery != "" {
		ext := filepath.Ext(decoded)
		base := strings.TrimSuffix(decoded, ext)
		safeQuery := strings.ReplaceAll(u.RawQuery, "/", "_")
		safeQuery = strings.ReplaceAll(safeQuery, "\\", "_")
		safeQuery = strings.ReplaceAll(safeQuery, ":", "_")
		safeQuery = strings.ReplaceAll(safeQuery, "?", "_")
		safeQuery = strings.ReplaceAll(safeQuery, "&", "_")
		decoded = base + "_" + safeQuery + ext
	}

	resolved := filepath.Join(fs.outputDir, host, decoded)

	cleanOutput := filepath.Clean(fs.outputDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(resolved), cleanOutput) {
		return filepath.Join(fs.outputDir, host, "_blocked")
	}

	return resolved
}

// PathForAPI returns the local path for a captured API response, or an empty
// string if the URL cannot be mapped safely.
func (fs *Filesystem) PathForAPI(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	cleanPath := u.Path
	if cleanPath == "" || cleanPath == "/" {
		return ""
	}
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = filepath.FromSlash(cleanPath)
	if strings.Contains(cleanPath, "..") {
		return ""
	}
	resolved := filepath.Join(fs.outputDir, cleanPath)
	if strings.HasSuffix(cleanPath, "/") || filepath.Ext(cleanPath) == "" {
		resolved += ".json"
	}
	cleanOutput := filepath.Clean(fs.outputDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(resolved), cleanOutput) {
		return ""
	}
	return resolved
}

// GetLocalPath returns the local path previously saved for rawURL, if any.
func (fs *Filesystem) GetLocalPath(rawURL string) (string, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	info, ok := fs.index[rawURL]
	if !ok {
		return "", false
	}
	return info.LocalPath, true
}
