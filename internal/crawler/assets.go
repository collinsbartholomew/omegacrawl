package crawler

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	netintercept "github.com/user/clone/internal/network"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

func (c *Crawler) downloadHTMLAssets(baseURL, pageHTML, htmlLocalPath string, netIntercept *netintercept.Interceptor, cdpSaved map[string]bool) {
	c.rewriter.SetBaseURL(baseURL)

	assetURLs := make(map[string]bool)
	tokenizer := html.NewTokenizer(strings.NewReader(pageHTML))
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		_, hasAttr := tokenizer.TagName()

		var attrs []struct{ k, v string }
		if hasAttr {
			for {
				k, v, more := tokenizer.TagAttr()
				attrs = append(attrs, struct{ k, v string }{string(k), string(v)})
				if !more {
					break
				}
			}
		}

		for _, attr := range attrs {
			ak := attr.k
			av := attr.v
			if av == "" || av == "#" || strings.HasPrefix(av, "javascript:") || strings.HasPrefix(av, "mailto:") || strings.HasPrefix(av, "data:") {
				continue
			}
			var resourceURL string
			switch ak {
			case "src", "href", "action", "poster", "data":
				resourceURL = av
			case "srcset":
				parts := strings.Split(av, ",")
				for _, part := range parts {
					urlPart := strings.TrimSpace(strings.Split(strings.TrimSpace(part), " ")[0])
					if urlPart != "" {
						absURL := rewrite.ResolveURL(baseURL, urlPart)
						if absURL != "" && isValidURL(absURL) {
							assetURLs[absURL] = true
						}
					}
				}
				continue
			default:
				if strings.HasPrefix(ak, "data-") && (strings.Contains(ak, "src") || strings.Contains(ak, "url") || strings.Contains(ak, "bg") || strings.Contains(ak, "image") || strings.Contains(ak, "lazy")) {
					resourceURL = av
				}
			}
			if resourceURL == "" {
				continue
			}
			absURL := rewrite.ResolveURL(baseURL, resourceURL)
			if absURL != "" && isValidURL(absURL) {

				if (ak == "href" || ak == "action") && isHTMLPageURL(absURL) {
					continue
				}
				assetURLs[absURL] = true
			}
		}
	}

	var g errgroup.Group
	g.SetLimit(5)
	for assetURL := range assetURLs {
		if cdpSaved[assetURL] {
			continue
		}
		if c.bloomFilter.HasSeen(assetURL) {
			continue
		}
		if !c.isAllowedDomain(assetURL) || c.isExcluded(assetURL) {
			continue
		}

		u := assetURL
		g.Go(func() error {
			resource, err := netIntercept.DownloadResourceViaHTTP(u)
			if err != nil || resource == nil || len(resource.Body) == 0 {
				return nil
			}
			if _, saved := c.saveCapturedResource(u, resource.Body, resource.MimeType, filepath.Dir(htmlLocalPath)); saved {
				c.metrics.IncAssetsCaptured()
				c.metrics.AddBytes(int64(len(resource.Body)))
			}
			return nil
		})
	}
	g.Wait()
}

// saveCapturedResource persists body for rawURL under a content-hash-derived
// path, or, if identical content was already saved under a different URL,
// records an alias mapping so rawURL still resolves to the existing file.
// It returns the local path and whether the content was newly saved.
func (c *Crawler) saveCapturedResource(rawURL string, body []byte, mimeType, htmlDir string) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	hashStr := strconv.FormatUint(xxhash.Sum64(body), 36)

	// First check the persistent bloom filter (never evicts)
	if !c.contentHashBloom.HasSeen(hashStr) {
		// Definitely new content - save it
		localPath, err := c.storage.SaveFile(rawURL, body, mimeType)
		if err != nil {
			util.LogError("asset save failed", err, zap.String("url", rawURL))
			return "", false
		}
		c.contentHashBloom.Add(hashStr)
		c.contentHashes.Put(hashStr, localPath)
		c.mapURLToLocal(rawURL, localPath, htmlDir)
		return localPath, true
	}

	// Bloom filter says we might have seen this hash - check LRU cache for fast path
	existing, added := c.contentHashes.PutIfAbsent(hashStr, "")
	if !added {
		if existing != "" {
			c.mapURLToLocal(rawURL, existing, htmlDir)
		}
		return existing, false
	}

	// LRU cache didn't have it (evicted), but bloom says we've seen it
	// This shouldn't happen often, but if it does, save again
	localPath, err := c.storage.SaveFile(rawURL, body, mimeType)
	if err != nil {
		util.LogError("asset save failed", err, zap.String("url", rawURL))
		return "", false
	}
	c.contentHashes.Put(hashStr, localPath)
	c.mapURLToLocal(rawURL, localPath, htmlDir)
	return localPath, true
}

func (c *Crawler) mapURLToLocal(rawURL, localPath, htmlDir string) {
	c.rewriter.AddMapping(rawURL, localPath)
	relPath, err := filepath.Rel(htmlDir, localPath)
	if err != nil {
		relPath = localPath
	}
	c.rewriter.AddAbsoluteToRelMapping(rawURL, filepath.ToSlash(relPath))
}
