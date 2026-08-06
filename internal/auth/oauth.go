package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/clone/internal/httpclient"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (am *AuthManager) oauthFlow(ctx context.Context, targetURL string) error {
	if am.cfg.OAuthConfig == nil {
		return fmt.Errorf("oauth config required for oauth auth type")
	}

	am.tokenMu.Lock()
	if am.token != nil && am.token.IsValid() {
		am.tokenMu.Unlock()
		return am.injectOAuthToken(ctx, targetURL)
	}

	if am.token != nil && am.token.RefreshToken != "" {
		refreshed, err := am.refreshOAuthToken(ctx, am.token.RefreshToken)
		if err == nil {

			if refreshed.RefreshToken == "" {
				refreshed.RefreshToken = am.token.RefreshToken
			}
			am.token = refreshed
			am.tokenMu.Unlock()
			util.LogInfo("refreshed oauth token")
			return am.injectOAuthToken(ctx, targetURL)
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

	return am.injectOAuthToken(ctx, targetURL)
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

func (am *AuthManager) injectOAuthToken(ctx context.Context, targetURL string) error {
	am.tokenMu.RLock()
	token := am.token
	am.tokenMu.RUnlock()

	if token == nil {
		return fmt.Errorf("no oauth token available")
	}

	return installScopedHeaders(ctx, targetURL, map[string]interface{}{
		"Authorization": token.TokenType + " " + token.AccessToken,
	})
}

// IsValid reports whether the token is non-empty and not near expiry.
func (t *OAuthToken) IsValid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	return time.Now().Before(t.ExpiresAt.Add(-30 * time.Second))
}
