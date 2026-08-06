package auth

import (
	"context"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (am *AuthManager) injectHeaders(ctx context.Context, targetURL string) error {
	if am.cfg.HeaderAuth == nil || len(am.cfg.HeaderAuth.Headers) == 0 {
		return nil
	}

	headers := make(map[string]interface{})
	for k, v := range am.cfg.HeaderAuth.Headers {
		headers[k] = v
	}
	if err := installScopedHeaders(ctx, targetURL, headers); err != nil {
		util.LogDebug("failed to inject auth headers", zap.Error(err))
		return err
	}

	util.LogInfo("injected custom auth headers", zap.Int("count", len(am.cfg.HeaderAuth.Headers)))
	return nil
}
