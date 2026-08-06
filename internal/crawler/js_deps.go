package crawler

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/clone/internal/jsanalyzer"
)

func (c *Crawler) resolveJSDependencies(htmlLocalPath, baseURL string) {
	htmlDir := filepath.Dir(htmlLocalPath)
	jsFiles := make(map[string]string)

	for filePath := range c.rewriter.GetMappings() {
		if strings.HasSuffix(filePath, ".js") {
			localPath := c.rewriter.GetMappings()[filePath]
			if localPath != "" {
				jsFiles[filePath] = localPath
			}
		}
	}

	for _, localPath := range jsFiles {
		jsData, err := os.ReadFile(localPath)
		if err != nil {
			continue
		}

		analyzedURLs := jsanalyzer.ExtractJSURLs(string(jsData), baseURL)

		for _, au := range analyzedURLs {
			if c.bloomFilter.HasSeen(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}

			resp, err := c.httpClient.Get(au.URL)
			if err != nil || resp.StatusCode != 200 {
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(body) == 0 {
				continue
			}

			if _, saved := c.saveCapturedResource(au.URL, body, resp.Header.Get("Content-Type"), htmlDir); saved {
				c.metrics.IncAssetsCaptured()
				c.metrics.AddBytes(int64(len(body)))

				if strings.HasSuffix(au.URL, ".js") {
					c.resolveJSDependenciesRecursive(au.URL, baseURL, htmlDir, 0)
				}
			}
		}

		htmlURLs := jsanalyzer.ExtractFromHTML(string(jsData), baseURL)
		for _, au := range htmlURLs {
			if c.bloomFilter.HasSeen(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}
			resp, err := c.httpClient.Get(au.URL)
			if err != nil || resp.StatusCode != 200 {
				if resp != nil {
					resp.Body.Close()
				}
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(body) == 0 {
				continue
			}

			savedPath, err := c.storage.SaveFile(au.URL, body, resp.Header.Get("Content-Type"))
			if err != nil {
				continue
			}
			c.rewriter.AddMapping(au.URL, savedPath)
			relPath, _ := filepath.Rel(htmlDir, savedPath)
			relPath = filepath.ToSlash(relPath)
			c.rewriter.AddAbsoluteToRelMapping(au.URL, relPath)

			c.metrics.IncAssetsCaptured()
			c.metrics.AddBytes(int64(len(body)))
		}
	}
}

func (c *Crawler) resolveJSDependenciesRecursive(jsURL, baseURL, htmlDir string, depth int) {
	if depth > 3 {
		return
	}

	if c.bloomFilter.HasSeen(jsURL) {
		return
	}
	if !c.isAllowedDomain(jsURL) || c.isExcluded(jsURL) {
		return
	}

	resp, err := c.httpClient.Get(jsURL)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		return
	}

	if _, saved := c.saveCapturedResource(jsURL, body, resp.Header.Get("Content-Type"), htmlDir); !saved {
		return
	}

	c.metrics.IncAssetsCaptured()
	c.metrics.AddBytes(int64(len(body)))

	analyzedURLs := jsanalyzer.ExtractJSURLs(string(body), baseURL)
	for _, au := range analyzedURLs {
		if strings.HasSuffix(au.URL, ".js") {
			c.resolveJSDependenciesRecursive(au.URL, baseURL, htmlDir, depth+1)
		} else {
			if c.bloomFilter.HasSeen(au.URL) {
				continue
			}
			if !c.isAllowedDomain(au.URL) || c.isExcluded(au.URL) {
				continue
			}
			resp2, err := c.httpClient.Get(au.URL)
			if err != nil || resp2.StatusCode != 200 {
				if resp2 != nil {
					resp2.Body.Close()
				}
				continue
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			if len(body2) == 0 {
				continue
			}

			if _, saved := c.saveCapturedResource(au.URL, body2, resp2.Header.Get("Content-Type"), htmlDir); saved {
				c.metrics.IncAssetsCaptured()
				c.metrics.AddBytes(int64(len(body2)))
			}
		}
	}
}
