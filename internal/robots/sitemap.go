package robots

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// GetSitemaps returns the cached sitemap URLs declared by the host's robots.txt.
func (rp *RobotsParser) GetSitemaps(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	robotsURL := u.Scheme + "://" + u.Hostname() + "/robots.txt"

	rp.mu.RLock()
	entry, ok := rp.cache[robotsURL]
	rp.mu.RUnlock()

	if !ok {
		return nil
	}

	return entry.Sitemaps
}

// FetchSitemapURLs fetches the given sitemap and returns the URLs listed in it.
func (rp *RobotsParser) FetchSitemapURLs(sitemapURL string) []string {
	var urls []string

	ctx, cancel := context.WithTimeout(rp.parentCtx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", sitemapURL, nil)
	if err != nil {
		return nil
	}
	resp, err := httpclient.GlobalClient().Do(req)
	if err != nil {
		util.LogDebug("failed to fetch sitemap", zap.String("url", sitemapURL), zap.Error(err))
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			util.LogDebug("failed to discard sitemap response body", zap.Error(copyErr), zap.String("url", sitemapURL))
		}
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil
	}

	bodyStr := string(body)

	if strings.Contains(bodyStr, "<urlset") || strings.Contains(bodyStr, "<sitemapindex") || strings.Contains(bodyStr, ":urlset") || strings.Contains(bodyStr, ":sitemapindex") {
		urlPattern := regexp.MustCompile(`<[^:>]*:?loc[^>]*>\s*(.*?)\s*</[^:>]*:?loc\s*>`)
		matches := urlPattern.FindAllStringSubmatch(bodyStr, -1)
		for _, match := range matches {
			if len(match) > 1 {
				u := strings.TrimSpace(match[1])
				if u != "" {
					urls = append(urls, u)
				}
			}
		}
	}

	return urls
}

// GetSitemapURLs returns all URLs from the host's declared sitemaps.
func (rp *RobotsParser) GetSitemapURLs(rawURL string) []string {
	sitemaps := rp.GetSitemaps(rawURL)
	if len(sitemaps) == 0 {
		return nil
	}

	var allURLs []string
	for _, sitemapURL := range sitemaps {
		urls := rp.FetchSitemapURLs(sitemapURL)
		allURLs = append(allURLs, urls...)
	}
	return allURLs
}
