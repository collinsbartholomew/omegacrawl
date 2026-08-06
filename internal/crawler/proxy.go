package crawler

import "math/rand"

func (c *Crawler) selectProxy() string {
	if c.cfg.Proxy != "" {
		return c.cfg.Proxy
	}
	if len(c.cfg.Proxies) > 0 {
		return c.cfg.Proxies[rand.Intn(len(c.cfg.Proxies))]
	}
	return ""
}
