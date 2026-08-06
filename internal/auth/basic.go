package auth

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (am *AuthManager) basicAuth(ctx context.Context, targetURL string) error {
	if am.cfg.BasicAuth == nil || am.cfg.BasicAuth.Username == "" {
		return fmt.Errorf("basic auth requires username and password")
	}

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(am.cfg.BasicAuth.Username+":"+am.cfg.BasicAuth.Password))

	util.LogInfo("injecting basic auth headers", zap.String("target", targetURL))

	return installScopedHeaders(ctx, targetURL, map[string]interface{}{
		"Authorization": auth,
	})
}
