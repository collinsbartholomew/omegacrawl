package auth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// installScopedHeaders adds the given headers to requests that target the host
// of targetURL, using Fetch-domain interception. Unlike session-wide
// Network.setExtraHTTPHeaders, this never leaks credentials (Authorization,
// bearer tokens, custom headers) to third-party hosts that a page loads. The
// original request headers, including cookies, are preserved and the auth
// headers are merged on top.
func installScopedHeaders(ctx context.Context, targetURL string, headers map[string]interface{}) error {
	u, err := url.Parse(targetURL)
	if err != nil || u.Host == "" {
		return err
	}
	pattern := u.Scheme + "://" + u.Host + "/*"

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		paused, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		merged := make(map[string]string, len(paused.Request.Headers)+len(headers))
		for k, v := range paused.Request.Headers {
			merged[k] = headerValue(v)
		}
		for k, v := range headers {
			merged[k] = headerValue(v)
		}
		entries := make([]*fetch.HeaderEntry, 0, len(merged))
		for k, v := range merged {
			entries = append(entries, &fetch.HeaderEntry{Name: k, Value: v})
		}
		if err := fetch.ContinueRequest(paused.RequestID).WithHeaders(entries).Do(ctx); err != nil {
			util.LogDebug("failed to continue intercepted request", zap.Error(err))
		}
	})

	return chromedp.Run(ctx, fetch.Enable().WithPatterns([]*fetch.RequestPattern{
		{URLPattern: pattern},
	}))
}

func headerValue(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
