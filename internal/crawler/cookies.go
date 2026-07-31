package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) setCookies(ctx context.Context) {
	if len(c.cfg.Cookies) == 0 {
		return
	}
	for _, cookie := range c.cfg.Cookies {
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			params := network.SetCookie(cookie.Name, cookie.Value)
			if cookie.Domain != "" {
				params = params.WithDomain(cookie.Domain)
			}
			if cookie.Path != "" {
				params = params.WithPath(cookie.Path)
			}
			if cookie.HTTPOnly {
				params = params.WithHTTPOnly(true)
			}
			return params.Do(ctx)
		})); err != nil {
			util.LogDebug("set cookie failed", zap.Error(err))
		}
	}
}

func (c *Crawler) saveCookieJar() {
	c.cookieMu.RLock()
	defer c.cookieMu.RUnlock()
	if len(c.cookieJar) == 0 {
		return
	}
	data, err := json.MarshalIndent(c.cookieJar, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(c.cfg.OutputDir, cookieJarFile)
	os.MkdirAll(c.cfg.OutputDir, 0755)
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to save cookie jar", err)
	}
}

func (c *Crawler) periodicCookieSave() {
	cookieTicker := time.NewTicker(5 * time.Minute)
	defer cookieTicker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.saveCookieJar()
			return
		case <-cookieTicker.C:
			c.saveCookieJar()
		}
	}
}

func (c *Crawler) loadCookieJar() {
	path := filepath.Join(c.cfg.OutputDir, cookieJarFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var jar map[string][]*http.Cookie
	if err := json.Unmarshal(data, &jar); err != nil {
		return
	}
	c.cookieMu.Lock()
	for domain, cookies := range jar {
		c.cookieJar[domain] = cookies
	}
	c.cookieMu.Unlock()
	util.LogInfo("loaded cookie jar", zap.Int("domains", len(jar)))
}

func (c *Crawler) persistCookies(ctx context.Context, urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	domain := parsed.Hostname()
	if domain == "" {
		return
	}

	var cookies []*http.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cctx context.Context) error {
		cdpCookies, err := network.GetCookies().Do(cctx)
		if err != nil {
			return err
		}
		cookies = util.CDPCookiesToHTTP(cdpCookies)
		return nil
	})); err != nil {
		util.LogDebug("failed to persist cookies", zap.Error(err))
		return
	}

	c.cookieMu.Lock()
	for _, ck := range cookies {
		ckDomain := domain
		if ck.Domain != "" {
			ckDomain = strings.TrimPrefix(ck.Domain, ".")
		}
		jar := c.cookieJar[ckDomain]
		jar = append(jar, ck)
		if len(jar) > maxCookiesPerDomain {
			jar = jar[len(jar)-maxCookiesPerDomain:]
		}
		c.cookieJar[ckDomain] = jar
	}
	c.cookieMu.Unlock()

	util.LogDebug("persisted cookies", zap.String("domain", domain), zap.Int("count", len(cookies)))
}

func (c *Crawler) injectPersistedCookies(ctx context.Context, urlStr string) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	domain := parsed.Hostname()
	if domain == "" {
		return
	}

	c.cookieMu.RLock()
	var allCookies []*http.Cookie
	seen := make(map[string]bool)
	for jarDomain, cookies := range c.cookieJar {
		if domain == jarDomain || strings.HasSuffix(domain, "."+jarDomain) {
			for _, ck := range cookies {
				key := ck.Name + "|" + ck.Domain + "|" + ck.Path
				if !seen[key] {
					seen[key] = true
					allCookies = append(allCookies, ck)
				}
			}
		}
	}
	c.cookieMu.RUnlock()

	for _, cookie := range allCookies {
		cp := network.SetCookie(cookie.Name, cookie.Value)
		if cookie.Domain != "" {
			cp = cp.WithDomain(cookie.Domain)
		}
		if cookie.Path != "" {
			cp = cp.WithPath(cookie.Path)
		}
		if cookie.Secure {
			cp = cp.WithSecure(true)
		}
		if cookie.HttpOnly {
			cp = cp.WithHTTPOnly(true)
		}
		if !cookie.Expires.IsZero() {
			expires := cdp.TimeSinceEpoch(cookie.Expires)
			cp = cp.WithExpires(&expires)
		}
		if err := chromedp.Run(ctx, cp); err != nil {
			util.LogDebug("failed to inject persisted cookie", zap.Error(err))
		}
	}

	if len(allCookies) > 0 {
		util.LogDebug("injected persisted cookies",
			zap.String("domain", domain),
			zap.Int("count", len(allCookies)),
		)
	}
}
