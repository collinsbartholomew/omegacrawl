package rewrite

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// RewriteCSS rewrites URLs in the given CSS content to local relative paths.
func (r *Rewriter) RewriteCSS(css []byte, cssLocalPath string) []byte {
	result := css
	cssDir := filepath.Dir(cssLocalPath)

	r.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range r.urlToLocal {
		mappings[k] = v
	}
	absToRel := make(map[string]string)
	for k, v := range r.absoluteToRel {
		absToRel[k] = v
	}
	r.mu.RUnlock()

	sortedURLs := make([]string, 0, len(mappings))
	for origURL := range mappings {
		sortedURLs = append(sortedURLs, origURL)
	}
	sort.Slice(sortedURLs, func(i, j int) bool {
		return len(sortedURLs[i]) > len(sortedURLs[j])
	})

	var pairs [][2][]byte
	for _, origURL := range sortedURLs {
		localPath := mappings[origURL]
		relPath, err := filepath.Rel(cssDir, localPath)
		if err != nil {
			relPath = localPath
		}
		relPath = filepath.ToSlash(relPath)

		pairs = append(pairs,
			[2][]byte{[]byte(`"` + origURL + `"`), []byte(`"` + relPath + `"`)},
			[2][]byte{[]byte(`'` + origURL + `'`), []byte(`'` + relPath + `'`)},
		)
	}
	if len(pairs) > 0 {
		result = batchReplace(result, pairs)
	}

	result = cssURLPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		start := bytes.IndexByte(match, '(')
		end := bytes.IndexByte(match, ')')
		if start < 0 || end < 0 {
			return match
		}

		urlStr := bytes.TrimSpace(match[start+1 : end])
		urlStr = bytes.Trim(urlStr, `"' `)
		urlStrStr := string(urlStr)

		if localPath, ok := mappings[urlStrStr]; ok {
			relPath, err := filepath.Rel(cssDir, localPath)
			if err != nil {
				relPath = localPath
			}
			relPath = filepath.ToSlash(relPath)

			quote := byte('"')
			if bytes.Contains(match, []byte(`'`)) {
				quote = byte('\'')
			}
			return []byte(`url(` + string(quote) + relPath + string(quote) + `)`)
		}

		if relPath, ok := absToRel[urlStrStr]; ok {
			quote := byte('"')
			if bytes.Contains(match, []byte(`'`)) {
				quote = byte('\'')
			}
			return []byte(`url(` + string(quote) + relPath + string(quote) + `)`)
		}
		return match
	})

	result = cssImportPattern.ReplaceAllFunc(result, func(match []byte) []byte {
		matches := cssImportPattern.FindSubmatch(match)
		if len(matches) > 1 {
			urlStr := string(matches[1])
			urlStr = strings.Trim(urlStr, `"' `)
			if localPath, ok := mappings[urlStr]; ok {
				relPath, err := filepath.Rel(cssDir, localPath)
				if err != nil {
					util.LogError("failed to compute relative path for CSS import", err, zap.String("baseDir", cssDir), zap.String("localPath", localPath))
					relPath = localPath
				}
				relPath = filepath.ToSlash(relPath)
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
			if relPath, ok := absToRel[urlStr]; ok {
				return bytes.Replace(match, matches[1], []byte(relPath), 1)
			}
		}
		return match
	})

	r.processCSSImportsRecursive(cssLocalPath, mappings)

	return result
}

func (r *Rewriter) processCSSImportsRecursive(cssPath string, mappings map[string]string) {
	processed := make(map[string]bool)
	r.processCSSImports(cssPath, mappings, processed)
}

func (r *Rewriter) processCSSImports(cssPath string, mappings map[string]string, processed map[string]bool) {
	if processed[cssPath] {
		return
	}
	processed[cssPath] = true

	cssData, err := os.ReadFile(cssPath)
	if err != nil {
		return
	}

	for _, match := range cssImportPattern.FindAllSubmatch(cssData, -1) {
		if len(match) < 2 {
			continue
		}
		importURL := string(match[1])
		importURL = strings.Trim(importURL, `"' `)
		localPath, ok := mappings[importURL]
		if !ok {
			continue
		}
		if processed[localPath] {
			continue
		}

		r.processCSSImports(localPath, mappings, processed)
	}
}

func (r *Rewriter) processCSSImportsForURL(cssLocalPath string) {
	r.mu.RLock()
	mappings := make(map[string]string)
	for k, v := range r.urlToLocal {
		mappings[k] = v
	}
	r.mu.RUnlock()
	r.processCSSImportsRecursive(cssLocalPath, mappings)
}

// ProcessFiles rewrites each file in place based on its declared type ("html" or "css").
func (r *Rewriter) ProcessFiles(files map[string]string) error {
	util.LogDebug("=== DEBUG: ProcessFiles called ===")
	for filePath, fileType := range files {
		data, err := os.ReadFile(filePath)
		if err != nil {
			util.LogError("failed to read file for rewriting", err, zap.String("path", filePath))
			continue
		}

		var rewritten []byte
		switch fileType {
		case "html":
			util.LogDebug("=== DEBUG: ProcessFiles rewriting HTML ===")
			util.LogDebug("ProcessFiles: rewriting HTML", zap.String("path", filePath))
			rewritten = r.RewriteHTML(data, filePath)
		case "css":
			util.LogDebug("=== DEBUG: ProcessFiles rewriting CSS ===")
			util.LogDebug("ProcessFiles: rewriting CSS", zap.String("path", filePath))
			rewritten = r.RewriteCSS(data, filePath)
			r.processCSSImportsForURL(filePath)
		default:
			continue
		}

		if err := os.WriteFile(filePath, rewritten, 0644); err != nil {
			util.LogError("failed to write rewritten file", err, zap.String("path", filePath))
			continue
		}
	}
	return nil
}
