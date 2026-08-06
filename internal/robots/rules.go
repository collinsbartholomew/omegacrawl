package robots

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// NewRobotsParser returns a RobotsParser with default caching and user-agent settings.
func NewRobotsParser() *RobotsParser {
	return &RobotsParser{
		cache:        make(map[string]*RobotsEntry),
		cacheTTL:     24 * time.Hour,
		transientTTL: 5 * time.Minute,
		userAgent:    "*",
		ruleCache:    make(map[string]*regexp.Regexp),
		parentCtx:    context.Background(),
	}
}

// SetContext sets the parent context used for robots.txt fetches.
func (rp *RobotsParser) SetContext(ctx context.Context) {
	rp.parentCtx = ctx
}

// SetUserAgent sets the user agent used to match robots.txt rules.
func (rp *RobotsParser) SetUserAgent(ua string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.userAgent = ua
}

// CanCrawl reports whether the URL may be crawled, along with the applicable crawl delay.
func (rp *RobotsParser) CanCrawl(rawURL string, userAgent string, cfg *config.Config) (bool, time.Duration) {
	if !cfg.RespectRobots {
		return true, cfg.CrawlDelay
	}

	// Honor the caller's user-agent for rule matching, falling back to the
	// parser default when none is supplied.
	ua := userAgent
	if ua == "" {
		ua = rp.userAgent
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return true, cfg.CrawlDelay
	}

	robotsURL := u.Scheme + "://" + u.Hostname() + "/robots.txt"

	rp.mu.RLock()
	entry, ok := rp.cache[robotsURL]
	rp.mu.RUnlock()

	if ok {
		ttl := rp.cacheTTL
		if entry.transient {
			ttl = rp.transientTTL
		}
		if time.Since(entry.fetchedAt) < ttl {
			return rp.evaluateRules(entry, u.Path, cfg)
		}
	}

	res, err, _ := rp.fetchGroup.Do(robotsURL, func() (interface{}, error) {
		entry := rp.fetchRobots(robotsURL, ua)
		rp.mu.Lock()
		rp.cache[robotsURL] = entry
		rp.mu.Unlock()
		return entry, nil
	})
	if err != nil {
		return false, 0
	}
	entry = res.(*RobotsEntry)

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

func (rp *RobotsParser) compileRule(rule string) *regexp.Regexp {
	rp.ruleCacheMu.RLock()
	if re, ok := rp.ruleCache[rule]; ok {
		rp.ruleCacheMu.RUnlock()
		return re
	}
	rp.ruleCacheMu.RUnlock()

	regexPattern := "^" + regexp.QuoteMeta(rule)
	regexPattern = strings.ReplaceAll(regexPattern, "\\*", ".*")

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil
	}

	rp.ruleCacheMu.Lock()
	rp.ruleCache[rule] = re
	rp.ruleCacheMu.Unlock()
	return re
}

func (rp *RobotsParser) matchPath(rule, path string) bool {
	if rule == "" {
		return false
	}

	if !strings.Contains(rule, "*") {
		return strings.HasPrefix(path, rule)
	}

	re := rp.compileRule(rule)
	if re == nil {
		return strings.HasPrefix(path, strings.TrimSuffix(rule, "*"))
	}

	return re.MatchString(path)
}

func (rp *RobotsParser) fetchRobots(robotsURL string, ua string) *RobotsEntry {
	entry := &RobotsEntry{
		fetchedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(rp.parentCtx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
	if err != nil {
		util.LogDebug("failed to create robots.txt request", zap.Error(err))
		return entry
	}

	resp, err := httpclient.GlobalClient().Do(req)
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
			transient: true,
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
			isMatch := rp.matchUserAgent(value, ua)

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
			if inOurAgent && value != "" {
				entry.Disallow = append(entry.Disallow, value)
			}
		case "allow":
			if inOurAgent && value != "" {
				entry.Allow = append(entry.Allow, value)
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

func (rp *RobotsParser) matchUserAgent(pattern, ua string) bool {
	if pattern == "*" {
		return true
	}

	lowerPattern := strings.ToLower(pattern)
	lowerUA := strings.ToLower(ua)

	return strings.Contains(lowerUA, lowerPattern)
}

// ClearExpired removes cached robots.txt entries that have exceeded the cache TTL.
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
