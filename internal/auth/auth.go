package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/cdproto/network"
	"go.uber.org/zap"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/util"
)

type AuthManager struct {
	cfg       *config.AuthConfig
	cookieJar map[string][]*http.Cookie
	jarMu     sync.RWMutex
	token     *OAuthToken
	tokenMu   sync.RWMutex
}

func NewAuthManager(cfg *config.AuthConfig) *AuthManager {
	return &AuthManager{
		cfg:       cfg,
		cookieJar: make(map[string][]*http.Cookie),
	}
}

func (am *AuthManager) Authenticate(ctx context.Context, targetURL string) error {
	if am.cfg == nil || !am.cfg.Enabled {
		return nil
	}

	switch am.cfg.Type {
	case "form":
		return am.formLogin(ctx, targetURL)
	case "basic":
		return am.basicAuth(ctx, targetURL)
	case "header":
		return am.injectHeaders(ctx, targetURL)
	case "oauth":
		return am.oauthFlow(ctx, targetURL)
	default:
		return fmt.Errorf("unknown auth type: %s", am.cfg.Type)
	}
}

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

	tabCtx, cancel := chromedp.NewContext(ctx)
	defer cancel()

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
		select {
		case <-time.After(am.cfg.WaitAfterLogin):
		case <-waitCtx.Done():
		}
	}

	parsedTarget, _ := url.Parse(targetURL)
	domain := parsedTarget.Hostname()
	if domain == "" {
		domain = "default"
	}

	var cookies []*http.Cookie
	if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(cctx context.Context) error {
		cdpCookies, err := network.GetCookies().Do(cctx)
		if err != nil {
			return err
		}
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

	return nil
}

func (am *AuthManager) basicAuth(ctx context.Context, targetURL string) error {
	if am.cfg.BasicAuth == nil || am.cfg.BasicAuth.Username == "" {
		return fmt.Errorf("basic auth requires username and password")
	}

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(am.cfg.BasicAuth.Username+":"+am.cfg.BasicAuth.Password))

	util.LogInfo("injecting basic auth headers", zap.String("target", targetURL))

	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetExtraHTTPHeaders(map[string]interface{}{
			"Authorization": auth,
		}).Do(ctx)
	}))
}

func (am *AuthManager) injectHeaders(ctx context.Context, targetURL string) error {
	if am.cfg.HeaderAuth == nil || len(am.cfg.HeaderAuth.Headers) == 0 {
		return nil
	}

	for k, v := range am.cfg.HeaderAuth.Headers {
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetExtraHTTPHeaders(map[string]interface{}{k: v}).Do(ctx)
		})); err != nil {
			util.LogDebug("failed to inject header", zap.String("key", k), zap.Error(err))
		}
	}

	util.LogInfo("injected custom auth headers", zap.Int("count", len(am.cfg.HeaderAuth.Headers)))
	return nil
}

func (am *AuthManager) oauthFlow(ctx context.Context, targetURL string) error {
	if am.cfg.OAuthConfig == nil {
		return fmt.Errorf("oauth config required for oauth auth type")
	}

	if am.token != nil && am.token.IsValid() {
		return am.injectOAuthToken(ctx)
	}

	token, err := am.exchangeOAuthToken(ctx)
	if err != nil {
		return fmt.Errorf("oauth token exchange failed: %w", err)
	}

	am.tokenMu.Lock()
	am.token = token
	am.tokenMu.Unlock()

	return am.injectOAuthToken(ctx)
}

func (am *AuthManager) exchangeOAuthToken(ctx context.Context) (*OAuthToken, error) {
	oc := am.cfg.OAuthConfig
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", oc.ClientID)
	data.Set("client_secret", oc.ClientSecret)
	if len(oc.Scopes) > 0 {
		data.Set("scope", strings.Join(oc.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(context.Background(), "POST", oc.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oauth token request failed: %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &OAuthToken{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		RefreshToken: tokenResp.RefreshToken,
	}, nil
}

func (am *AuthManager) injectOAuthToken(ctx context.Context) error {
	am.tokenMu.RLock()
	token := am.token
	am.tokenMu.RUnlock()

	if token == nil {
		return fmt.Errorf("no oauth token available")
	}

	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetExtraHTTPHeaders(map[string]interface{}{
			"Authorization": token.TokenType + " " + token.AccessToken,
		}).Do(ctx)
	}))
}

func (am *AuthManager) storeCookies(domain string, cookies []*http.Cookie) {
	am.jarMu.Lock()
	defer am.jarMu.Unlock()
	am.cookieJar[domain] = cookies
}

func (am *AuthManager) GetCookies(domain string) []*http.Cookie {
	am.jarMu.RLock()
	defer am.jarMu.RUnlock()
	return am.cookieJar[domain]
}

func (am *AuthManager) HasValidSession(domain string) bool {
	cookies := am.GetCookies(domain)
	if len(cookies) == 0 {
		return false
	}
	for _, c := range cookies {
		if !c.Expires.IsZero() && time.Now().After(c.Expires) {
			continue
		}
		return true
	}
	return false
}

type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
}

func (t *OAuthToken) IsValid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	return time.Now().Before(t.ExpiresAt.Add(-30 * time.Second))
}