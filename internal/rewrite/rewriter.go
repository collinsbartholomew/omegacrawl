package rewrite

import (
	"strings"
	"sync"
)

// Rewriter rewrites HTML and CSS to replace remote URLs with local file paths.
type Rewriter struct {
	urlToLocal    map[string]string
	absoluteToRel map[string]string
	cssFiles      map[string]bool
	baseURL       string
	mu            sync.RWMutex
}

// NewRewriter returns an empty Rewriter ready for URL mappings.
func NewRewriter() *Rewriter {
	return &Rewriter{
		urlToLocal:    make(map[string]string),
		absoluteToRel: make(map[string]string),
		cssFiles:      make(map[string]bool),
	}
}

// SetBaseURL sets the base URL used when resolving relative references.
func (r *Rewriter) SetBaseURL(baseURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.baseURL = baseURL
}

// AddMapping records that an original URL maps to a local file path.
func (r *Rewriter) AddMapping(originalURL, localPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urlToLocal[originalURL] = localPath
	if strings.HasSuffix(localPath, ".css") {
		r.cssFiles[localPath] = true
	}
}

// GetCSSFiles returns a copy of the map of local CSS file paths.
func (r *Rewriter) GetCSSFiles() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]bool)
	for k, v := range r.cssFiles {
		result[k] = v
	}
	return result
}

// GetMappings returns a copy of the URL-to-local-path mapping.
func (r *Rewriter) GetMappings() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range r.urlToLocal {
		result[k] = v
	}
	return result
}

// GetAbsoluteToRel returns a copy of the absolute-URL-to-relative-path mapping.
func (r *Rewriter) GetAbsoluteToRel() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range r.absoluteToRel {
		result[k] = v
	}
	return result
}

// AddAbsoluteToRelMapping records that an absolute URL should be rewritten to a relative path.
func (r *Rewriter) AddAbsoluteToRelMapping(absoluteURL, relativePath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.absoluteToRel[absoluteURL] = relativePath
}
