package crawler

import (
	"math/rand"
	"net/url"
	"strings"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/queue"
)

func cfgUserAgent(cfg *config.Config) string {
	if cfg.RotateUserAgents && len(cfg.UserAgents) > 0 {
		return cfg.UserAgents[rand.Intn(len(cfg.UserAgents))]
	}
	return cfg.UserAgent
}

func getHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func isValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// isHTMLPageURL reports whether absURL points at an HTML page or navigation
// endpoint (directory, extensionless path, or a known server-side template
// extension) rather than a static asset.
func isHTMLPageURL(absURL string) bool {
	u, err := url.Parse(absURL)
	if err != nil {
		return false
	}
	path := strings.TrimSuffix(u.Path, "/")
	last := path[strings.LastIndex(path, "/")+1:]
	if last == "" {
		return true
	}
	dot := strings.LastIndex(last, ".")
	if dot < 0 {
		return true
	}
	switch strings.ToLower(last[dot+1:]) {
	case "html", "htm", "php", "asp", "aspx", "jsp", "jspx", "cfm":
		return true
	}
	return false
}

func (c *Crawler) normalizeURL(rawURL string) string {
	if c.cfg.NormalizeURLs {
		return queue.NormalizeURL(rawURL)
	}
	return rawURL
}

func (c *Crawler) isAllowedDomain(rawURL string) bool {
	if len(c.cfg.AllowedDomains) == 0 {
		return true
	}
	host := getHost(rawURL)
	for _, domain := range c.cfg.AllowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (c *Crawler) isExcluded(rawURL string) bool {
	return c.excludeFn(rawURL)
}

// blockedPatternMatch reports whether rawURL matches one of the configured
// block patterns (same glob semantics as the browser-level URL blocking).
func (c *Crawler) blockedPatternMatch(rawURL string) bool {
	for _, p := range c.cfg.BlockedURLPatterns {
		if queue.URLGlobMatch(p, rawURL) {
			return true
		}
	}
	return false
}

func (c *Crawler) isSameDomain(original, target string) bool {
	if original == "" {
		return true
	}
	return getHost(original) == getHost(target)
}

// isSeedPage reports whether urlStr is one of the configured seed URLs after
// normalization. Seed pages get site-root aggregate outputs (e.g. a host-level
// article.json / structured-data.json) that downstream pipelines read as the
// site-level document.
func (c *Crawler) isSeedPage(urlStr string) bool {
	c.seedsMu.RLock()
	seeds := append([]string{}, c.seeds...)
	c.seedsMu.RUnlock()
	if len(seeds) == 0 {
		return false
	}
	target := c.normalizeURL(queue.NormalizeAndClean(urlStr))
	for _, s := range seeds {
		if c.normalizeURL(queue.NormalizeAndClean(s)) == target {
			return true
		}
	}
	return false
}
