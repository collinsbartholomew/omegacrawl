package crawler

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) writeSitemap() {
	c.routeMu.RLock()
	urls := make([]string, 0, len(c.discoveredRoutes))
	for u := range c.discoveredRoutes {
		urls = append(urls, u)
	}
	c.routeMu.RUnlock()
	if len(urls) == 0 {
		return
	}
	sort.Strings(urls)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range urls {
		escaped := strings.ReplaceAll(u, "&", "&amp;")
		escaped = strings.ReplaceAll(escaped, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		b.WriteString(fmt.Sprintf("  <url><loc>%s</loc></url>\n", escaped))
	}
	b.WriteString("</urlset>\n")
	path := c.cfg.OutputDir + "/sitemap.xml"
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		util.LogError("failed to write sitemap.xml", err)
		return
	}
	util.LogInfo("wrote sitemap", zap.String("path", path), zap.Int("urls", len(urls)))
}
