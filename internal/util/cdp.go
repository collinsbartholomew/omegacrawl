package util

import (
	"net/http"
	"time"

	"github.com/chromedp/cdproto/network"
)

// CDPCookiesToHTTP converts a slice of Chrome DevTools Protocol cookies into
// standard net/http cookies.
func CDPCookiesToHTTP(cdpCookies []*network.Cookie) []*http.Cookie {
	cookies := make([]*http.Cookie, 0, len(cdpCookies))
	for _, c := range cdpCookies {
		cookies = append(cookies, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
			Expires:  time.Unix(int64(c.Expires), 0),
		})
	}
	return cookies
}
