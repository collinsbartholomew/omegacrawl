package robots

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/util"
)

type RobotsEntry struct {
	Disallow    []string
	Allow       []string
	CrawlDelay time.Duration
	Sitemaps    []string
	fetchedAt   time.Time
}

type RobotsParser struct {
	cache      map[string]*RobotsEntry
	mu         sync.RWMutex
	cacheTTL   time.Duration
	userAgent  string
}

func NewRobotsParser() *RobotsParser {
	return &RobotsParser{
		cache:     make(map[string]*RobotsEntry),
		cacheTTL:  24 * time.Hour,
		userAgent: "*",
	}
}

func (rp *RobotsParser) SetUserAgent(ua string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.userAgent = ua
}

func (rp *RobotsParser) CanCrawl(rawURL string, userAgent string, cfg *config.Config) (bool, time.Duration) {
	if !cfg.RespectRobots {
		return true, cfg.CrawlDelay
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return true, cfg.CrawlDelay
	}

	robotsURL := u.Scheme + "://" + u.Hostname() + "/robots.txt"

	rp.mu.RLock()
	entry, ok := rp.cache[robotsURL]
	rp.mu.RUnlock()

	if ok && time.Since(entry.fetchedAt) < rp.cacheTTL {
		return rp.evaluateRules(entry, u.Path, cfg)
	}

	entry = rp.fetchRobots(robotsURL)
	rp.mu.Lock()
	rp.cache[robotsURL] = entry
	rp.mu.Unlock()

	return rp.evaluateRules(entry, u.Path, cfg)
}

func (rp *RobotsParser) evaluateRules(entry *RobotsEntry, path string, cfg *config.Config) (bool, time.Duration) {
	if path == "" {
		path = "/"
	}

	allowScore, disallowScore := 0, 0
	allowPath, disallowPath := "", ""

	for _, rule := range entry.Allow {
		if rp.matchPath(rule, path) {
			score := len(rule)
			if score > allowScore {
				allowScore = score
				allowPath = rule
			}
		}
	}

	for _, rule := range entry.Disallow {
		if rp.matchPath(rule, path) {
			score := len(rule)
			if score > disallowScore {
				disallowScore = score
				disallowPath = rule
			}
		}
	}

	if allowScore > 0 || disallowScore > 0 {
		if allowScore >= disallowScore {
			util.LogDebug("robots.txt: allowed",
				zap.String("path", path),
				zap.String("rule", allowPath),
			)
		} else {
			util.LogDebug("robots.txt: blocked",
				zap.String("path", path),
				zap.String("rule", disallowPath),
			)
			return false, 0
		}
	}

	delay := cfg.CrawlDelay
	if entry.CrawlDelay > 0 {
		delay = entry.CrawlDelay
	}

	return true, delay
}

func (rp *RobotsParser) matchPath(rule, path string) bool {
	if rule == "" {
		return false
	}

	if !strings.Contains(rule, "*") {
		return strings.HasPrefix(path, rule)
	}

	regexPattern := "^" + regexp.QuoteMeta(rule)
	regexPattern = strings.ReplaceAll(regexPattern, "\\*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "\\$", "$")

	matched, err := regexp.MatchString(regexPattern, path)
	if err != nil {
		return strings.HasPrefix(path, strings.TrimSuffix(rule, "*"))
	}

	return matched
}

func (rp *RobotsParser) fetchRobots(robotsURL string) *RobotsEntry {
	entry := &RobotsEntry{
		fetchedAt: time.Now(),
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(robotsURL)
	if err != nil {
		util.LogDebug("failed to fetch robots.txt", zap.String("url", robotsURL), zap.Error(err))
		return entry
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return entry
	}

	if resp.StatusCode >= 500 {
		return &RobotsEntry{
			fetchedAt: time.Now(),
			Disallow:  []string{"/"},
		}
	}

	if resp.StatusCode != 200 {
		return entry
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
	if err != nil {
		return entry
	}

	lines := strings.Split(string(body), "\n")
	inOurAgent := false
	matchedSpecific := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			isWildcard := value == "*"
			isMatch := rp.matchUserAgent(value)

			if matchedSpecific {
				inOurAgent = false
			} else if !isWildcard && isMatch {
				matchedSpecific = true
				inOurAgent = true
			} else if isWildcard && !matchedSpecific {
				inOurAgent = true
			} else {
				inOurAgent = false
			}
		case "disallow":
			if inOurAgent {
				if value == "" {
					entry.Disallow = append(entry.Disallow, "/")
				} else {
					entry.Disallow = append(entry.Disallow, value)
				}
			}
		case "allow":
			if inOurAgent {
				if value == "" {
					entry.Allow = append(entry.Allow, "/")
				} else {
					entry.Allow = append(entry.Allow, value)
				}
			}
		case "crawl-delay":
			if inOurAgent {
				delay, err := time.ParseDuration(value + "s")
				if err == nil {
					entry.CrawlDelay = delay
				}
			}
		case "sitemap":
			entry.Sitemaps = append(entry.Sitemaps, value)
		}
	}

	return entry
}

func (rp *RobotsParser) matchUserAgent(pattern string) bool {
	if pattern == "*" {
		return true
	}

	lowerPattern := strings.ToLower(pattern)
	lowerUA := strings.ToLower(rp.userAgent)

	return strings.Contains(lowerUA, lowerPattern)
}

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

func (rp *RobotsParser) ClearExpired() {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	now := time.Now()
	for key, entry := range rp.cache {
		if now.Sub(entry.fetchedAt) > rp.cacheTTL {
			delete(rp.cache, key)
		}
	}
}

func (rp *RobotsParser) FetchSitemapURLs(sitemapURL string) []string {
	var urls []string

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(sitemapURL)
	if err != nil {
		util.LogDebug("failed to fetch sitemap", zap.String("url", sitemapURL), zap.Error(err))
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
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
