package captcha

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Solver) poll2Captcha(ctx context.Context, taskID string) (*SolveResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	first := true
	for {
		if !first {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pollTick.C:
			}
		}
		first = false

		// 2Captcha legacy polling: GET /res.php?key=...&action=get&id=...
		// returns plain text: "OK|<solution>", "CAPCHA_NOT_READY" while
		// processing, or an "ERROR_..." string. The JSON /getTaskResult
		// endpoint (used by AntiCaptcha/CapMonster) does not accept legacy
		// query parameters.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/res.php", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		query := req.URL.Query()
		query.Add("key", s.apiKey)
		query.Add("action", "get")
		query.Add("id", taskID)
		req.URL.RawQuery = query.Encode()

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("2captcha API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		text := strings.TrimSpace(string(bodyBytes))
		switch {
		case strings.HasPrefix(text, "OK|"):
			token := strings.TrimPrefix(text, "OK|")
			if token != "" && !is2CaptchaError(token) {
				return &SolveResponse{Token: token, Solved: true}, nil
			}
			return nil, fmt.Errorf("task finished without a solution: %s", text)
		case text == "CAPCHA_NOT_READY":
			continue
		default:
			return nil, fmt.Errorf("task failed with response: %s", text)
		}
	}
}

// is2CaptchaError reports whether s is a 2Captcha-style error string that the
// API returns in place of a solution (e.g. "ERROR_CAPTCHA_UNSOLVABLE").
func is2CaptchaError(s string) bool {
	return strings.HasPrefix(s, "ERROR_") || s == "CAPCHA_NOT_READY"
}
