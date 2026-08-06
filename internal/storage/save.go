package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// SaveFile writes raw file content to disk under the path derived from
// rawURL and records it in the index. It returns the local path.
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

// SaveHTML writes an HTML document to disk and records it in the index.
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

// SaveScreenshot writes a PNG screenshot for the given URL to disk.
func (fs *Filesystem) SaveScreenshot(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + ".png"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

// SavePDF writes a PDF document for the given URL to disk.
func (fs *Filesystem) SavePDF(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + ".pdf"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

// SaveShadowDOM writes captured shadow DOM content for the given URL to disk.
func (fs *Filesystem) SaveShadowDOM(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + "-shadowdom.json"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

// SaveStructuredData writes per-page structured data (JSON-LD, OG, meta)
// next to the page's HTML file. Paths are derived from the page URL so
// distinct pages never overwrite each other's data.
func (fs *Filesystem) SaveStructuredData(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + "-structured-data.json"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

// SaveArticle writes the per-page readability article extraction next to the
// page's HTML file, mirroring the shadow-DOM naming convention.
func (fs *Filesystem) SaveArticle(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + "-article.json"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}

// SaveSingleFile writes the per-page SingleFile-style snapshot next to the
// page's HTML file, mirroring the shadow-DOM naming convention.
func (fs *Filesystem) SaveSingleFile(rawURL string, data []byte) (string, error) {
	path := fs.PathForURL(rawURL) + "-singlefile.html"
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return path, os.WriteFile(path, data, 0644)
}
