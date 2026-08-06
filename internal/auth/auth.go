package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/user/clone/internal/config"
)

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

// Close releases any resources held by the AuthManager.
func (am *AuthManager) Close() {
	am.formMu.Lock()
	defer am.formMu.Unlock()
	if am.formTabCancel != nil {
		am.formTabCancel()
	}
	am.formTabCtx = nil
	am.formTabCancel = nil
}
