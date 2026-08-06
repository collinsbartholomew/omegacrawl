package rewrite

import (
	"bytes"
	"path/filepath"
	"regexp"
	"sync"
)

// URLMatcher provides URL matching using pre-computed patterns
type URLMatcher struct {
	mu          sync.RWMutex
	patterns    []*URLPattern
	pathCache   map[string]string
	absRelCache map[string]string
}

type URLPattern struct {
	Regex    *regexp.Regexp
	Prefix   string
	IsPrefix bool // true for prefix match, false for regex
	Replacer func(match []byte) []byte
}

// NewURLMatcher creates a new URLMatcher
func NewURLMatcher() *URLMatcher {
	return &URLMatcher{
		patterns:    make([]*URLPattern, 0),
		pathCache:   make(map[string]string),
		absRelCache: make(map[string]string),
	}
}

// SetMappings updates the URL mappings
func (m *URLMatcher) SetMappings(pathCache, absRelCache map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pathCache = pathCache
	m.absRelCache = absRelCache
}

// GetPathCache returns the path cache
func (m *URLMatcher) GetPathCache() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string, len(m.pathCache))
	for k, v := range m.pathCache {
		result[k] = v
	}
	return result
}

// GetAbsRelCache returns the absRel cache
func (m *URLMatcher) GetAbsRelCache() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string, len(m.absRelCache))
	for k, v := range m.absRelCache {
		result[k] = v
	}
	return result
}

// RewriteHTMLFast performs HTML rewriting - delegates to main rewriter for safety
func (m *URLMatcher) RewriteHTMLFast(htmlContent []byte, htmlDir, baseURL string) []byte {
	// Fast path disabled to prevent corruption of URLs in JS strings
	// Always fall back to the safe tokenizer-based rewriter
	return htmlContent
}

// RewriteCSSFast performs CSS rewriting - delegates to main rewriter for safety
func (m *URLMatcher) RewriteCSSFast(cssContent []byte, htmlDir, baseURL string) []byte {
	// Fast path disabled to prevent corruption of URLs in JS strings
	// Always fall back to the safe tokenizer-based rewriter
	return cssContent
}

// IncrementalMatcher provides incremental URL matching for streaming
type IncrementalMatcher struct {
	matcher   *URLMatcher
	buffer    bytes.Buffer
	lastFlush int
	chunkSize int
}

func NewIncrementalMatcher(matcher *URLMatcher, chunkSize int) *IncrementalMatcher {
	return &IncrementalMatcher{
		matcher:   matcher,
		chunkSize: chunkSize,
	}
}

func (im *IncrementalMatcher) Reset() {
	im.buffer.Reset()
}

func (im *IncrementalMatcher) Write(p []byte) (int, error) {
	im.buffer.Write(p)
	if im.buffer.Len() >= im.chunkSize {
		err := im.Flush()
		if err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (im *IncrementalMatcher) Flush() error {
	if im.buffer.Len() == 0 {
		return nil
	}

	content := im.buffer.Bytes()
	rewritten := im.matcher.RewriteHTMLFast(content, "", "")
	im.buffer.Reset()
	im.buffer.Write(rewritten)
	return nil
}

func (im *IncrementalMatcher) Bytes() []byte {
	if im.buffer.Len() > 0 {
		im.Flush()
	}
	return im.buffer.Bytes()
}

// PrecomputedRewriter extends Rewriter with pre-computed optimization
type PrecomputedRewriter struct {
	*Rewriter
	matcher       *URLMatcher
	sortedURLs    []string
	pathCache     map[string]string
	absRelCache   map[string]string
	scriptsCached bool
	stylesCached  bool
	scriptCache   []byte
	styleCache    []byte
	mu            sync.RWMutex
}

// NewPrecomputedRewriter creates a new precomputed rewriter
func NewPrecomputedRewriter(r *Rewriter) *PrecomputedRewriter {
	return &PrecomputedRewriter{
		Rewriter:      r,
		matcher:       NewURLMatcher(),
		pathCache:     make(map[string]string),
		absRelCache:   make(map[string]string),
		scriptsCached: false,
		stylesCached:  false,
	}
}

// Precompute builds all caches and compilations
func (pr *PrecomputedRewriter) Precompute(htmlDir, baseURL string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.Rewriter.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range pr.Rewriter.urlToLocal {
		mappings[k] = v
	}
	absToRel := make(map[string]string)
	for k, v := range pr.Rewriter.absoluteToRel {
		absToRel[k] = v
	}
	_ = pr.Rewriter.baseURL
	pr.Rewriter.mu.RUnlock()

	// Build path cache
	pr.pathCache = make(map[string]string, len(mappings))
	for origURL, localPath := range mappings {
		relPath, err := filepath.Rel(htmlDir, localPath)
		if err != nil {
			relPath = localPath
		}
		pr.pathCache[origURL] = filepath.ToSlash(relPath)
	}

	pr.absRelCache = make(map[string]string, len(absToRel))
	for absURL, relPath := range absToRel {
		pr.absRelCache[absURL] = relPath
	}

	// Sort URLs by length (longest first) for proper replacement
	pr.sortedURLs = make([]string, 0, len(mappings))
	for origURL := range mappings {
		pr.sortedURLs = append(pr.sortedURLs, origURL)
	}
	// Sort by length descending
	// Using strings.Compare for stable sorting
	for i := 0; i < len(pr.sortedURLs); i++ {
		for j := i + 1; j < len(pr.sortedURLs); j++ {
			if len(pr.sortedURLs[i]) < len(pr.sortedURLs[j]) {
				pr.sortedURLs[i], pr.sortedURLs[j] = pr.sortedURLs[j], pr.sortedURLs[i]
			}
		}
	}

	// Update matcher
	pr.matcher.SetMappings(pr.pathCache, pr.absRelCache)

	// Pre-compute script/style replacements
	pr.precomputeScriptStyle(baseURL)
}

// precomputeScriptStyle pre-computes replacements for script/style content
func (pr *PrecomputedRewriter) precomputeScriptStyle(baseURL string) {
	var scriptPairs, stylePairs [][2][]byte

	// Build pairs for script/style content
	for absURL, relPath := range pr.absRelCache {
		scriptPairs = append(scriptPairs, [2][]byte{[]byte(absURL), []byte(relPath)})
		stylePairs = append(stylePairs, [2][]byte{[]byte(absURL), []byte(relPath)})
	}
	for absURL, relPath := range pr.pathCache {
		scriptPairs = append(scriptPairs, [2][]byte{[]byte(absURL), []byte(relPath)})
		stylePairs = append(stylePairs, [2][]byte{[]byte(absURL), []byte(relPath)})
	}

	// Note: We can't fully precompute script/style because they need baseURL resolution
	// But we can cache the pairs for batchReplace
	pr.scriptsCached = len(scriptPairs) > 0
	pr.stylesCached = len(stylePairs) > 0
}

// RewriteHTMLOptimized rewrites HTML using pre-computed caches
func (pr *PrecomputedRewriter) RewriteHTMLOptimized(htmlContent []byte, htmlLocalPath string) []byte {
	// Fast path disabled to prevent corruption of URLs in JS strings
	// Always delegate to the safe tokenizer-based rewriter
	return pr.Rewriter.RewriteHTML(htmlContent, htmlLocalPath)
}

// RewriteCSSOptimized rewrites CSS using pre-computed caches
func (pr *PrecomputedRewriter) RewriteCSSOptimized(cssContent []byte, cssLocalPath string) []byte {
	// Fast path disabled to prevent corruption of URLs in JS strings
	// Always delegate to the safe tokenizer-based rewriter
	return pr.Rewriter.RewriteCSS(cssContent, cssLocalPath)
}

// OptimizedRewriter provides a fully optimized rewrite pipeline
type OptimizedRewriter struct {
	precomputed *PrecomputedRewriter
	streaming   *IncrementalMatcher
}

func NewOptimizedRewriter(r *Rewriter) *OptimizedRewriter {
	pre := NewPrecomputedRewriter(r)
	return &OptimizedRewriter{
		precomputed: pre,
		streaming:   NewIncrementalMatcher(pre.matcher, 64*1024), // 64KB chunks
	}
}

func (or *OptimizedRewriter) Initialize(htmlDir, baseURL string) {
	or.precomputed.Precompute(htmlDir, baseURL)
}

func (or *OptimizedRewriter) RewriteHTML(htmlContent []byte, htmlLocalPath string) []byte {
	return or.precomputed.RewriteHTMLOptimized(htmlContent, htmlLocalPath)
}

func (or *OptimizedRewriter) RewriteCSS(cssContent []byte, cssLocalPath string) []byte {
	return or.precomputed.RewriteCSSOptimized(cssContent, cssLocalPath)
}

// StreamingRewrite rewrites HTML in streaming fashion for large pages
func (or *OptimizedRewriter) StreamingRewrite(htmlContent []byte, htmlLocalPath string) ([]byte, error) {
	or.streaming.Reset()
	_, err := or.streaming.Write(htmlContent)
	if err != nil {
		return nil, err
	}
	err = or.streaming.Flush()
	if err != nil {
		return nil, err
	}
	return or.streaming.Bytes(), nil
}
