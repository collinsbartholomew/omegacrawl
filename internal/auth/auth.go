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

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
)

// AuthManager handles authentication for crawl targets.
type AuthManager struct {
	cfg       *config.AuthConfig
	cookieJar map[string][]*http.Cookie
	jarMu     sync.RWMutex
	token     *OAuthToken
	tokenMu   sync.RWMutex

	formTabOnce   sync.Once
	formTabCtx    context.Context
	formTabCancel context.CancelFunc
}

// NewAuthManager creates an AuthManager from the given auth configuration.
func NewAuthManager(cfg *config.AuthConfig) *AuthManager {
	return &AuthManager{
		cfg:       cfg,
		cookieJar: make(map[string][]*http.Cookie),
	}
}

// Authenticate applies the configured authentication flow to the target URL.
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

	am.formTabOnce.Do(func() {
		am.formTabCtx, am.formTabCancel = chromedp.NewContext(ctx)
	})
	tabCtx := am.formTabCtx

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
		cdpCookies, err := network.GetCookies().Do(cctx)
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

	// Fixed Bug B7: release the form tab context as soon as the login flow
	// completes. Cookies are persisted in the jar, so the dedicated tab is no
	// longer needed; previously it lived for the entire crawl duration,
	// consuming CDP target resources. The sync.Once is reset so a later
	// re-authentication can create a fresh tab.
	am.formTabCancel()
	am.formTabCtx = nil
	am.formTabCancel = nil
	am.formTabOnce = sync.Once{}

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

	headers := make(map[string]interface{})
	for k, v := range am.cfg.HeaderAuth.Headers {
		headers[k] = v
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetExtraHTTPHeaders(headers).Do(ctx)
	})); err != nil {
		util.LogDebug("failed to inject auth headers", zap.Error(err))
	}

	util.LogInfo("injected custom auth headers", zap.Int("count", len(am.cfg.HeaderAuth.Headers)))
	return nil
}

func (am *AuthManager) oauthFlow(ctx context.Context, targetURL string) error {
	if am.cfg.OAuthConfig == nil {
		return fmt.Errorf("oauth config required for oauth auth type")
	}

	am.tokenMu.Lock()
	if am.token != nil && am.token.IsValid() {
		am.tokenMu.Unlock()
		return am.injectOAuthToken(ctx)
	}

	// Token is missing or expiring. Prefer refreshing over a full exchange when
	// a refresh token is available, so long-lived crawls do not renegotiate
	// client credentials on every expiry.
	if am.token != nil && am.token.RefreshToken != "" {
		refreshed, err := am.refreshOAuthToken(ctx, am.token.RefreshToken)
		if err == nil {
			// Servers that do not rotate refresh tokens omit a new one in the
			// response; preserve the existing token so refresh keeps working.
			if refreshed.RefreshToken == "" {
				refreshed.RefreshToken = am.token.RefreshToken
			}
			am.token = refreshed
			am.tokenMu.Unlock()
			util.LogInfo("refreshed oauth token")
			return am.injectOAuthToken(ctx)
		}
		util.LogDebug("oauth token refresh failed, falling back to full exchange", zap.Error(err))
	}

	token, err := am.exchangeOAuthToken(ctx)
	if err != nil {
		am.tokenMu.Unlock()
		return fmt.Errorf("oauth token exchange failed: %w", err)
	}

	am.token = token
	am.tokenMu.Unlock()

	return am.injectOAuthToken(ctx)
}

func (am *AuthManager) refreshOAuthToken(ctx context.Context, refreshToken string) (*OAuthToken, error) {
	oc := am.cfg.OAuthConfig
	refreshURL := oc.RefreshURL
	if refreshURL == "" {
		refreshURL = oc.TokenURL
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", oc.ClientID)
	data.Set("client_secret", oc.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpclient.GlobalClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oauth token refresh failed: %d", resp.StatusCode)
	}

	var tokenResp oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return tokenFromResponse(tokenResp)
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

	req, err := http.NewRequestWithContext(ctx, "POST", oc.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpclient.GlobalClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oauth token request failed: %d", resp.StatusCode)
	}

	var tokenResp oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return tokenFromResponse(tokenResp)
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

// tokenFromResponse validates a decoded token response before it can be stored.
// A missing access_token usually means the server returned an OAuth error body
// (e.g. {"error":"invalid_grant"}) with HTTP 200; surfacing that as an error
// prevents a header like "Bearer " from being injected into every request.
func tokenFromResponse(resp oauthTokenResponse) (*OAuthToken, error) {
	if resp.AccessToken == "" {
		if resp.Error != "" {
			return nil, fmt.Errorf("oauth server error: %s", resp.Error)
		}
		return nil, fmt.Errorf("oauth response missing access_token")
	}
	tokenType := resp.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return &OAuthToken{
		AccessToken:  resp.AccessToken,
		TokenType:    tokenType,
		ExpiresAt:    expiryTime(resp.ExpiresIn),
		RefreshToken: resp.RefreshToken,
	}, nil
}

// expiryTime converts a server-provided lifetime (seconds) into an ExpiresAt
// timestamp. A missing or zero lifetime falls back to a conservative 5-minute
// validity so the token is not immediately expired and refreshable in time.
func expiryTime(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Now().Add(5 * time.Minute)
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
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

// GetCookies returns the cookies stored for the given domain.
func (am *AuthManager) GetCookies(domain string) []*http.Cookie {
	am.jarMu.RLock()
	defer am.jarMu.RUnlock()
	return am.cookieJar[domain]
}

// HasValidSession reports whether the stored cookies for the domain are still valid.
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

// OAuthToken holds the credentials for an OAuth client-credentials token.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
}

// IsValid reports whether the token is non-empty and not near expiry.
func (t *OAuthToken) IsValid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	return time.Now().Before(t.ExpiresAt.Add(-30 * time.Second))
}

// Close releases any resources held by the AuthManager.
func (am *AuthManager) Close() {
	if am.formTabCancel != nil {
		am.formTabCancel()
	}
	am.formTabOnce = sync.Once{} // Allow re-initialization
}
