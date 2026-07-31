package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/httpclient"
)

// Provider identifies a CAPTCHA solving service.
type Provider string

const (
	// Provider2Captcha uses the 2captcha service.
	Provider2Captcha Provider = "2captcha"
	// ProviderAntiCaptcha uses the Anti-Captcha service.
	ProviderAntiCaptcha Provider = "anticaptcha"
	// ProviderCapMonster uses the CapMonster service.
	ProviderCapMonster Provider = "capmonster"
)

// CAPTCHAType identifies the type of CAPTCHA to solve.
type CAPTCHAType string

const (
	// TypeRecaptchaV2 is a reCAPTCHA v2 challenge.
	TypeRecaptchaV2 CAPTCHAType = "recaptcha_v2"
	// TypeRecaptchaV3 is a reCAPTCHA v3 challenge.
	TypeRecaptchaV3 CAPTCHAType = "recaptcha_v3"
	// TypeHCaptcha is an hCaptcha challenge.
	TypeHCaptcha CAPTCHAType = "hcaptcha"
	// TypeImageCaptcha is an image-based CAPTCHA.
	TypeImageCaptcha CAPTCHAType = "image_captcha"
	// TypeTurnstile is a Cloudflare Turnstile challenge.
	TypeTurnstile CAPTCHAType = "turnstile"
)

// Solver submits and polls CAPTCHA solving tasks with a configured provider.
type Solver struct {
	provider Provider
	apiKey   string
	client   *http.Client
	baseURL  string
	timeout  time.Duration
	retryCnt int
}

// SolveRequest describes a CAPTCHA to be solved.
type SolveRequest struct {
	SiteURL  string      `json:"siteurl"`
	SiteKey  string      `json:"sitekey"`
	URL      string      `json:"url"`
	Type     CAPTCHAType `json:"type"`
	PageHTML string      `json:"page_html"`
	Proxy    string      `json:"proxy"`
}

// SolveResponse holds the result of a solved CAPTCHA.
type SolveResponse struct {
	Token     string `json:"token"`
	CaptchaID string `json:"captcha_id"`
	Solved    bool   `json:"solved"`
}

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
	var requestBody map[string]interface{}
	switch s.provider {
	case Provider2Captcha:
		requestBody = map[string]interface{}{"key": s.apiKey}
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

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/getTaskResult", nil)
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
			return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// 2Captcha reports readiness with status=1 and the token in "request".
		// Error strings (e.g. "ERROR_CAPTCHA_UNSOLVABLE") also arrive in
		// "request" when status=1 and must not be treated as solutions.
		if status, ok := result["status"]; ok {
			switch v := status.(type) {
			case float64:
				if v == 1 {
					if token, ok := result["request"].(string); ok && len(token) > 3 && !is2CaptchaError(token) {
						return &SolveResponse{Token: token, Solved: true}, nil
					}
					return nil, fmt.Errorf("task finished without a solution: %v", result)
				}
				if v == 0 {
					continue // not ready yet
				}
				return nil, fmt.Errorf("task failed with status: %v", status)
			case string:
				switch v {
				case "completed", "ready":
					token, captchaID := extractSolution(result)
					if token == "" {
						return nil, fmt.Errorf("task finished without a solution: %v", result)
					}
					return &SolveResponse{Token: token, CaptchaID: captchaID, Solved: true}, nil
				case "processing":
					continue
				default:
					return nil, fmt.Errorf("task failed with status: %v", status)
				}
			}
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}

func (s *Solver) pollAntiCaptcha(ctx context.Context, taskID string) (*SolveResponse, error) {
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

		requestBody := map[string]interface{}{
			"clientKey": s.apiKey,
			"taskId":    taskID,
		}
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal poll request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/getTaskResult", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

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
			return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
			return nil, fmt.Errorf("provider error %d", int(errorID))
		}

		if status, ok := result["status"].(string); ok {
			switch status {
			case "ready":
				token, captchaID := extractSolution(result)
				if token == "" {
					return nil, fmt.Errorf("task finished without a solution: %v", result)
				}
				return &SolveResponse{Token: token, CaptchaID: captchaID, Solved: true}, nil
			case "processing":
				continue
			default:
				return nil, fmt.Errorf("task failed with status: %v", status)
			}
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}

// extractSolution pulls the solved token and optional captcha id out of a
// provider result, handling both the 2Captcha ("solution.text" /
// "solution.captchaIds") and AntiCaptcha ("solution.gRecaptchaResponse") shapes.
func extractSolution(result map[string]interface{}) (token, captchaID string) {
	solution, _ := result["solution"].(map[string]interface{})
	if solution == nil {
		if t, ok := result["request"].(string); ok && len(t) > 3 {
			return t, ""
		}
		if t, ok := result["response"].(string); ok && len(t) > 3 {
			return t, ""
		}
		return "", ""
	}

	if t, ok := solution["text"].(string); ok && len(t) > 3 {
		token = t
	}
	if token == "" {
		for _, k := range []string{"gRecaptchaResponse", "response", "captcha"} {
			if t, ok := solution[k].(string); ok && len(t) > 3 {
				token = t
				break
			}
		}
	}
	if ids, ok := solution["captchaIds"].([]interface{}); ok && len(ids) > 0 {
		if id, ok := ids[0].(string); ok {
			captchaID = id
		}
	}
	return token, captchaID
}

func (s *Solver) pollCapMonster(ctx context.Context, taskID string) (*SolveResponse, error) {
	// CapMonster uses the same JSON API as AntiCaptcha: POST clientKey+taskId
	// to /getTaskResult and poll until status becomes "ready".
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

		requestBody := map[string]interface{}{
			"clientKey": s.apiKey,
			"taskId":    taskID,
		}
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal poll request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/getTaskResult", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

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
			return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
			return nil, fmt.Errorf("provider error %d", int(errorID))
		}

		if status, ok := result["status"].(string); ok {
			switch status {
			case "ready":
				token, captchaID := extractSolution(result)
				if token == "" {
					if response, ok := result["response"].(string); ok && len(response) > 3 && !is2CaptchaError(response) {
						token = response
					}
				}
				if token == "" {
					return nil, fmt.Errorf("task finished without a solution: %v", result)
				}
				return &SolveResponse{Token: token, CaptchaID: captchaID, Solved: true}, nil
			case "processing":
				continue
			default:
				return nil, fmt.Errorf("task failed with status: %v", status)
			}
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}

// is2CaptchaError reports whether s is a 2Captcha-style error string that the
// API returns in place of a solution (e.g. "ERROR_CAPTCHA_UNSOLVABLE").
func is2CaptchaError(s string) bool {
	return strings.HasPrefix(s, "ERROR_") || s == "CAPCHA_NOT_READY"
}

// InjectSolution injects the solved token into CAPTCHA fields on the page.
func (s *Solver) InjectSolution(ctx context.Context, token string) error {
	safeToken, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	script := fmt.Sprintf(`
		(function(token) {
			var recaptcha = document.getElementById('g-recaptcha-response');
			if (recaptcha) { recaptcha.innerHTML = token; }
			var hcaptcha = document.querySelector('[data-hcaptcha-response]');
			if (hcaptcha) { hcaptcha.innerHTML = token; }
			var turnstile = document.querySelector('[data-turnstile-response]');
			if (turnstile) { turnstile.innerHTML = token; }
		})(%s)
	`, string(safeToken))

	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}
