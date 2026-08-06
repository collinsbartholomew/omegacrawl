package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (am *AuthManager) formLogin(ctx context.Context, targetURL string) error {
	if am.cfg.LoginURL == "" {
		return fmt.Errorf("login_url required for form auth")
	}
	if am.cfg.Username == "" || am.cfg.Password == "" {
		return fmt.Errorf("username and password required for form auth")
	}

	util.LogInfo("performing form login",
		zap.String("login_url", am.cfg.LoginURL),
		zap.String("target", targetURL),
	)

	am.formMu.Lock()
	if am.formTabCancel != nil {
		am.formTabCancel()
	}
	am.formTabCtx, am.formTabCancel = chromedp.NewContext(ctx)
	tabCtx := am.formTabCtx
	am.formMu.Unlock()

	if err := chromedp.Run(tabCtx, chromedp.Navigate(am.cfg.LoginURL)); err != nil {
		return fmt.Errorf("navigate to login page: %w", err)
	}

	if am.cfg.WaitAfterLogin == 0 {
		am.cfg.WaitAfterLogin = 3 * time.Second
	}

	waitCtx, waitCancel := context.WithTimeout(tabCtx, am.cfg.WaitAfterLogin)
	defer waitCancel()

	actions := []chromedp.Action{}
	for selector, value := range am.cfg.FormFields {
		if strings.Contains(strings.ToLower(selector), "user") || strings.Contains(strings.ToLower(selector), "email") {
			actions = append(actions, chromedp.SendKeys(selector, am.cfg.Username))
		} else if strings.Contains(strings.ToLower(selector), "pass") {
			actions = append(actions, chromedp.SendKeys(selector, am.cfg.Password))
		} else {
			actions = append(actions, chromedp.SendKeys(selector, value))
		}
	}

	if am.cfg.SubmitSelector != "" {
		actions = append(actions, chromedp.Click(am.cfg.SubmitSelector))
	}

	if len(actions) > 0 {
		if err := chromedp.Run(waitCtx, actions...); err != nil {
			return fmt.Errorf("form interaction failed: %w", err)
		}
	}

	if am.cfg.WaitAfterLogin > 0 {
		waitTimer := time.NewTimer(am.cfg.WaitAfterLogin)
		select {
		case <-waitTimer.C:
		case <-waitCtx.Done():
			if !waitTimer.Stop() {
				<-waitTimer.C
			}
		}
	}

	parsedTarget, _ := url.Parse(targetURL)
	domain := parsedTarget.Hostname()
	if domain == "" {
		domain = "default"
	}

	var cookies []*http.Cookie
	if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(cctx context.Context) error {
		cdpCookies, err := network.GetCookies().WithURLs([]string{targetURL}).Do(cctx)
		if err != nil {
			return err
		}
		cookies = util.CDPCookiesToHTTP(cdpCookies)
		return nil
	})); err != nil {
		util.LogDebug("failed to get cookies after login", zap.Error(err))
	} else {
		am.storeCookies(domain, cookies)
		util.LogInfo("form login successful, cookies persisted",
			zap.String("domain", domain),
			zap.Int("cookies", len(cookies)),
		)
	}

	// Release the tab context after login is complete to avoid leaking CDP resources.
	// It will be recreated on the next call if re-authentication is needed.
	am.formMu.Lock()
	am.formTabCancel()
	am.formTabCtx = nil
	am.formTabCancel = nil
	am.formMu.Unlock()

	return nil
}
