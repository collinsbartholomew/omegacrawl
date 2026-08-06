package auth

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/user/clone/internal/config"
)

// AuthManager handles authentication for crawl targets.
type AuthManager struct {
	cfg       *config.AuthConfig
	cookieJar map[string][]*http.Cookie
	jarMu     sync.RWMutex
	token     *OAuthToken
	tokenMu   sync.RWMutex

	// formMu guards the formTab* fields, which are created during formLogin and
	// reset by Close. Without it a Close racing a formLogin corrupts the tab
	// context/cancel and the Once.
	formMu        sync.Mutex
	formTabCtx    context.Context
	formTabCancel context.CancelFunc
}

// OAuthToken holds the credentials for an OAuth client-credentials token.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
}
