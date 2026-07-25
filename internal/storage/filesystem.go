package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/util"
)

type FileInfo struct {
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	SHA256    string `json:"sha256"`
	MimeType  string `json:"mime_type"`
	Size      int    `json:"size"`
}

type Filesystem struct {
	outputDir string
	index     map[string]*FileInfo
	mu        sync.RWMutex
}

func NewFilesystem(cfg *config.Config) *Filesystem {
	return &Filesystem{
		outputDir: cfg.OutputDir,
		index:     make(map[string]*FileInfo),
	}
}

func (fs *Filesystem) SaveFile(rawURL string, data []byte, mimeType string) (string, error) {
	path := fs.PathForURL(rawURL)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	info := &FileInfo{
		URL:       rawURL,
		LocalPath: path,
		SHA256:    hex.EncodeToString(hash[:]),
		MimeType:  mimeType,
		Size:      len(data),
	}

	fs.mu.Lock()
	fs.index[rawURL] = info
	fs.mu.Unlock()

	util.LogDebug("saved file",
		zap.String("url", rawURL),
		zap.String("path", path),
		zap.Int("size", len(data)),
	)

	return path, nil
}

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

	// If path ends with /, treat as directory: /wp/nest/ -> wp/nest/index.html
	if strings.HasSuffix(decoded, "/") {
		decoded += "index.html"
	}

	decoded = strings.TrimPrefix(decoded, "/")

	// If path has no file extension, treat as directory page to avoid conflicts
	// e.g. /wp/nest saved as file prevents /wp/nest/documentation from creating dir
	baseName := decoded
	if idx := strings.LastIndex(decoded, "/"); idx >= 0 {
		baseName = decoded[idx+1:]
	}
	if !strings.Contains(baseName, ".") {
		decoded += "/index.html"
	}

	decoded = filepath.FromSlash(decoded)

	// SECURITY: Block path traversal after decoding
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

	// SECURITY: Verify path stays within output directory
	cleanOutput := filepath.Clean(fs.outputDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(resolved), cleanOutput) {
		return filepath.Join(fs.outputDir, host, "_blocked")
	}

	return resolved
}

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

func (fs *Filesystem) SaveHTML(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	info := &FileInfo{
		URL:       rawURL,
		LocalPath: path,
		SHA256:    hex.EncodeToString(hash[:]),
		MimeType:  "text/html",
		Size:      len(data),
	}

	fs.mu.Lock()
	fs.index[rawURL] = info
	fs.mu.Unlock()

	util.LogDebug("saved HTML", zap.String("url", rawURL), zap.String("path", path))
	return path, nil
}

func (fs *Filesystem) SaveScreenshot(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + ".png"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

func (fs *Filesystem) SavePDF(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + ".pdf"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

func (fs *Filesystem) SaveShadowDOM(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + "-shadowdom.json"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

func (fs *Filesystem) GetLocalPath(rawURL string) (string, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	info, ok := fs.index[rawURL]
	if !ok {
		return "", false
	}
	return info.LocalPath, true
}

func (fs *Filesystem) WriteIndex() error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if err := os.MkdirAll(fs.outputDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(fs.index, "", "  ")
	if err != nil {
		return err
	}

	indexPath := filepath.Join(fs.outputDir, "index.json")
	return os.WriteFile(indexPath, data, 0644)
}
