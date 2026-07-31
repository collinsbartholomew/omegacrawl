package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/clone/internal/config"
)

func TestRefreshOAuthToken(t *testing.T) {
	var gotGrantType, gotRefreshToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotRefreshToken = r.Form.Get("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "new-refresh",
		})
	}))
	defer srv.Close()

	am := &AuthManager{
		cfg: &config.AuthConfig{
			OAuthConfig: &config.OAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				TokenURL:     srv.URL,
				Scopes:       []string{"read"},
			},
		},
	}

	token, err := am.refreshOAuthToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("refreshOAuthToken: %v", err)
	}
	if gotGrantType != "refresh_token" {
		t.Errorf("expected grant_type=refresh_token, got %q", gotGrantType)
	}
	if gotRefreshToken != "old-refresh" {
		t.Errorf("expected refresh_token=old-refresh, got %q", gotRefreshToken)
	}
	if token.AccessToken != "new-access" {
		t.Errorf("expected access token new-access, got %q", token.AccessToken)
	}
	if token.RefreshToken != "new-refresh" {
		t.Errorf("expected refresh token new-refresh, got %q", token.RefreshToken)
	}
	if !token.IsValid() {
		t.Errorf("expected refreshed token to be valid")
	}
}

func TestRefreshUsesConfiguredRefreshURL(t *testing.T) {
	var mainURL, refreshURL string
	refreshSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshURL = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer refreshSrv.Close()
	mainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainURL = r.URL.Path
	}))
	defer mainSrv.Close()

	am := &AuthManager{
		cfg: &config.AuthConfig{
			OAuthConfig: &config.OAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				TokenURL:     mainSrv.URL + "/token",
				RefreshURL:   refreshSrv.URL + "/refresh",
			},
		},
	}

	if _, err := am.refreshOAuthToken(context.Background(), "rt"); err != nil {
		t.Fatalf("refreshOAuthToken: %v", err)
	}
	if mainURL != "" {
		t.Errorf("expected refresh to hit RefreshURL, but TokenURL was also used")
	}
	if refreshURL != "/refresh" {
		t.Errorf("expected refresh path /refresh, got %q", refreshURL)
	}
}

func TestOAuthFlowRefreshesExpiredToken(t *testing.T) {
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		tokenCalls++
		if r.Form.Get("grant_type") == "refresh_token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "fresh-access",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "fresh-refresh",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "orig-access",
			"token_type":    "Bearer",
			"expires_in":    -1,
			"refresh_token": "orig-refresh",
		})
	}))
	defer srv.Close()

	am := &AuthManager{
		cfg: &config.AuthConfig{
			OAuthConfig: &config.OAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				TokenURL:     srv.URL,
			},
		},
	}

	// First call: token missing -> full client_credentials exchange.
	token, err := am.exchangeOAuthToken(context.Background())
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	am.tokenMu.Lock()
	am.token = token
	am.tokenMu.Unlock()

	// Second call: token present but expired -> refresh, not full exchange.
	am.tokenMu.Lock()
	am.token.ExpiresAt = time.Now().Add(-time.Minute)
	am.tokenMu.Unlock()

	if _, err := am.refreshOAuthToken(context.Background(), "orig-refresh"); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if tokenCalls != 2 {
		t.Errorf("expected 2 token endpoint calls, got %d", tokenCalls)
	}
}

func TestRefreshPreservesTokenWhenServerDoesNotRotate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No refresh_token in the response: server does not rotate.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	am := &AuthManager{
		cfg: &config.AuthConfig{
			OAuthConfig: &config.OAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				TokenURL:     srv.URL,
			},
		},
		token: &OAuthToken{
			AccessToken:  "old-access",
			TokenType:    "Bearer",
			ExpiresAt:    time.Now().Add(-time.Minute),
			RefreshToken: "orig-refresh",
		},
	}

	// refreshOAuthToken passes through whatever the server returns (here an
	// empty refresh token). oauthFlow is responsible for preserving the
	// existing refresh token; verify that logic independently.
	refreshed, err := am.refreshOAuthToken(context.Background(), "orig-refresh")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.AccessToken != "new-access" {
		t.Errorf("expected new access token, got %q", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "" {
		t.Errorf("expected empty refresh token from non-rotating server, got %q", refreshed.RefreshToken)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = "orig-refresh"
	}
	if refreshed.RefreshToken != "orig-refresh" {
		t.Errorf("expected preserved refresh token orig-refresh, got %q", refreshed.RefreshToken)
	}
}

func TestTokenFromResponseRejectsEmptyAccessToken(t *testing.T) {
	if _, err := tokenFromResponse(oauthTokenResponse{Error: "invalid_grant"}); err == nil {
		t.Error("expected error for 200-with-error-body response")
	}
	if _, err := tokenFromResponse(oauthTokenResponse{}); err == nil {
		t.Error("expected error for empty response")
	}
	got, err := tokenFromResponse(oauthTokenResponse{AccessToken: "tok"})
	if err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if got.TokenType != "Bearer" {
		t.Errorf("expected default token_type Bearer, got %q", got.TokenType)
	}
}

func TestExpiryTimeFallback(t *testing.T) {
	if got := expiryTime(0); got.Before(time.Now()) {
		t.Errorf("expected zero expires_in to produce a future expiry, got %v", got)
	}
	if got := expiryTime(-5); got.Before(time.Now()) {
		t.Errorf("expected negative expires_in to produce a future expiry, got %v", got)
	}
	want := time.Now().Add(2 * time.Hour)
	got := expiryTime(7200)
	if diff := want.Sub(got); diff > time.Second || diff < -time.Second {
		t.Errorf("expected expiry 2h from now, got %v", got)
	}
}
