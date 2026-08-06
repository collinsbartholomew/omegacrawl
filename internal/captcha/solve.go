package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/httpclient"
)

// NewSolver creates a Solver for the given CAPTCHA config, or nil if disabled.
func NewSolver(cfg *config.CAPTCHAConfig) *Solver {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	var baseURL string
	switch cfg.Provider {
	case "2captcha":
		baseURL = "https://2captcha.com"
	case "anticaptcha":
		baseURL = "https://api.anti-captcha.com"
	case "capmonster":
		baseURL = "https://api.capmonster.cloud"
	default:
		baseURL = "https://2captcha.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	retryCnt := cfg.RetryCount
	if retryCnt < 0 {
		retryCnt = 0
	}
	return &Solver{
		provider: Provider(cfg.Provider),
		apiKey:   cfg.APIKey,
		baseURL:  baseURL,
		timeout:  timeout,
		retryCnt: retryCnt,
		client:   httpclient.GlobalClient(),
	}
}

// Solve submits a CAPTCHA solving task and waits for the result.
func (s *Solver) Solve(ctx context.Context, req SolveRequest) (*SolveResponse, error) {
	if s == nil {
		return nil, fmt.Errorf("captcha solver not configured")
	}

	taskType := "RecaptchaV2Task"
	switch req.Type {
	case TypeRecaptchaV2:
		taskType = "RecaptchaV2Task"
	case TypeRecaptchaV3:
		taskType = "RecaptchaV3Task"
	case TypeHCaptcha:
		taskType = "HCaptchaTask"
	case TypeTurnstile:
		taskType = "TurnstileTask"
	case TypeImageCaptcha:
		taskType = "ImageToTextTask"
	}

	task := map[string]interface{}{
		"type":        taskType,
		"websiteURL":  req.URL,
		"websiteKey":  req.SiteKey,
		"isInvisible": false,
		"proxyType":   "none",
	}

	return s.createTaskAndPoll(ctx, task, nil)
}

func (s *Solver) createTaskAndPoll(ctx context.Context, task map[string]interface{}, pollFn func(context.Context, string) (*SolveResponse, error)) (*SolveResponse, error) {
	poll := pollFn
	if poll == nil {
		switch s.provider {
		case Provider2Captcha:
			poll = s.poll2Captcha
		case ProviderAntiCaptcha:
			poll = s.pollAntiCaptcha
		case ProviderCapMonster:
			poll = s.pollCapMonster
		default:
			return nil, fmt.Errorf("unsupported provider: %s", s.provider)
		}
	}

	var taskID string
	var err error
	for attempt := 0; attempt <= s.retryCnt; attempt++ {
		taskID, err = s.submitTask(ctx, task)
		if err == nil {
			break
		}
		if attempt == s.retryCnt {
			return nil, err
		}
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}

	return poll(ctx, taskID)
}

// submitTask creates a CAPTCHA task on the provider, handling each provider's
// distinct request/response shape:
//   - 2Captcha uses a "key" field and returns the id in "request".
//   - AntiCaptcha and CapMonster use a "clientKey" field and return a numeric
//     "taskId".
//
// Fixed Bug B8: previously all providers shared a single code path that sent
// an "apiKey" field and looked for a string "captchaId", which AntiCaptcha
// never returned.
func (s *Solver) submitTask(ctx context.Context, task map[string]interface{}) (string, error) {
	// 2Captcha uses a distinct legacy form API (POST /in.php, plain-text
	// "OK|<taskId>" responses) rather than the JSON /createTask shape shared
	// by AntiCaptcha and CapMonster.
	if s.provider == Provider2Captcha {
		return s.submitTask2Captcha(ctx, task)
	}

	var requestBody map[string]interface{}
	switch s.provider {
	case ProviderAntiCaptcha, ProviderCapMonster:
		requestBody = map[string]interface{}{"clientKey": s.apiKey}
	default:
		return "", fmt.Errorf("unsupported provider: %s", s.provider)
	}
	for k, v := range task {
		requestBody[k] = v
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal task request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/createTask", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read error response: %w", err)
		}
		return "", fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
		return "", fmt.Errorf("provider error %d", int(errorID))
	}

	for _, idField := range []string{"taskId", "request", "captchaId"} {
		if id, ok := result[idField]; ok {
			switch v := id.(type) {
			case string:
				if v != "" {
					return v, nil
				}
			case float64:
				if v > 0 {
					return fmt.Sprintf("%d", int(v)), nil
				}
			}
		}
	}

	return "", fmt.Errorf("missing task id in response: %v", result)
}

// submitTask2Captcha creates a task using the 2Captcha legacy API: a
// form-encoded POST to /in.php that returns a plain-text "OK|<taskId>" on
// success or an "ERROR_..." string on failure. The JSON /createTask endpoint
// is not used for 2Captcha because it lives on api.2captcha.com with a
// different auth field (clientKey) and response shape (requestId).
func (s *Solver) submitTask2Captcha(ctx context.Context, task map[string]interface{}) (string, error) {
	form := url.Values{}
	form.Set("key", s.apiKey)

	// The legacy 2Captcha API names the site-key parameter differently per
	// challenge type: reCAPTCHA uses "googlekey", hCaptcha and Turnstile use
	// "sitekey". Image captchas take a base64 "body" instead of a site key.
	keyField := "googlekey"
	switch task["type"] {
	case "RecaptchaV2Task":
		form.Set("method", "userrecaptcha")
	case "RecaptchaV3Task":
		form.Set("method", "userrecaptchav3")
	case "HCaptchaTask":
		form.Set("method", "hcaptcha")
		keyField = "sitekey"
	case "TurnstileTask":
		form.Set("method", "turnstile")
		keyField = "sitekey"
	case "ImageToTextTask":
		// Note: image captchas need a base64 "body" parameter that Solve()
		// never supplies, so ImageToTextTask is not wired end-to-end and will
		// fail at the provider if configured.
		form.Set("method", "base64")
		keyField = ""
	default:
		form.Set("method", "userrecaptcha")
	}
	if u, ok := task["websiteURL"].(string); ok && u != "" {
		form.Set("pageurl", u)
	}
	if keyField != "" {
		if k, ok := task["websiteKey"].(string); ok && k != "" {
			form.Set(keyField, k)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/in.php", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("2captcha API error (status %d): %s", resp.StatusCode, string(body))
	}

	text := strings.TrimSpace(string(body))
	if strings.HasPrefix(text, "OK|") {
		if id := strings.TrimPrefix(text, "OK|"); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("2captcha task creation failed: %s", text)
}
