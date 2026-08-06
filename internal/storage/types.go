package storage

import "sync"

// FileInfo describes a file saved to disk, recording its source URL and
// metadata such as SHA256 hash, MIME type and size.
type FileInfo struct {
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	SHA256    string `json:"sha256"`
	MimeType  string `json:"mime_type"`
	Size      int    `json:"size"`
}

// Filesystem stores crawled content on disk and keeps an in-memory index of
// saved files keyed by their original URL.
type Filesystem struct {
	outputDir string
	index     map[string]*FileInfo
	mu        sync.RWMutex
}
