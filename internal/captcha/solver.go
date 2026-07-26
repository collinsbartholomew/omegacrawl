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

type Provider string

const (
	Provider2Captcha    Provider = "2captcha"
	ProviderAntiCaptcha Provider = "anticaptcha"
	ProviderCapMonster  Provider = "capmonster"
)

type CAPTCHAType string

const (
	TypeRecaptchaV2  CAPTCHAType = "recaptcha_v2"
	TypeRecaptchaV3  CAPTCHAType = "recaptcha_v3"
	TypeHCaptcha     CAPTCHAType = "hcaptcha"
	TypeImageCaptcha CAPTCHAType = "image_captcha"
	TypeTurnstile    CAPTCHAType = "turnstile"
)

type Solver struct {
	provider  Provider
	apiKey    string
	client    *http.Client
	baseURL   string
	timeout   time.Duration
	retryCnt  int
}

type SolveRequest struct {
	SiteURL  string      `json:"siteurl"`
	SiteKey  string      `json:"sitekey"`
	URL      string      `json:"url"`
	Type     CAPTCHAType `json:"type"`
	PageHTML string      `json:"page_html"`
	Proxy    string      `json:"proxy"`
}

type SolveResponse struct {
	Token    string `json:"token"`
	CaptchaID string `json:"captcha_id"`
	Solved   bool   `json:"solved"`
}

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
	return &Solver{
		provider: Provider(cfg.Provider),
		apiKey:   cfg.APIKey,
		baseURL:  baseURL,
		timeout:  cfg.Timeout,
		retryCnt: cfg.RetryCount,
		client: httpclient.GlobalClient(),
	}
}

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

	return s.createTaskAndPoll(ctx, task)
}

func (s *Solver) createTaskAndPoll(ctx context.Context, task map[string]interface{}) (*SolveResponse, error) {
	var endpoint string
	switch s.provider {
	case Provider2Captcha, ProviderAntiCaptcha:
		endpoint = s.baseURL + "/createTask"
		return s.handle2Captcha(ctx, endpoint, task)
	case ProviderCapMonster:
		endpoint = s.baseURL + "/createTask"
		return s.handleCapMonster(ctx, endpoint, task)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", s.provider)
	}
}

func (s *Solver) handle2Captcha(ctx context.Context, endpoint string, task map[string]interface{}) (*SolveResponse, error) {
	requestBody := map[string]interface{}{
		"apiKey": s.apiKey,
	}
	for k, v := range task {
		requestBody[k] = v
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
		return nil, fmt.Errorf("provider error %d", int(errorID))
	}

	var taskID string
	if captchaID, ok := result["captchaId"].(string); ok {
		taskID = captchaID
	} else {
		return nil, fmt.Errorf("missing captchaId in response")
	}

	return s.poll2Captcha(ctx, taskID)
}

func (s *Solver) handleCapMonster(ctx context.Context, endpoint string, task map[string]interface{}) (*SolveResponse, error) {
	requestBody := map[string]interface{}{
		"apiKey": s.apiKey,
	}
	for k, v := range task {
		requestBody[k] = v
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
		return nil, fmt.Errorf("provider error %d", int(errorID))
	}

	var taskID string
	if id, ok := result["taskId"].(string); ok {
		taskID = id
	} else {
		return nil, fmt.Errorf("missing taskId in response")
	}

	return s.pollCapMonster(ctx, taskID)
}

func (s *Solver) poll2Captcha(ctx context.Context, taskID string) (*SolveResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pollTick.C:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/getTaskResult", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		query := req.URL.Query()
		query.Add("key", s.apiKey)
		query.Add("action", "getTaskResult")
		query.Add("task", taskID)
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

		if status, ok := result["status"].(string); ok {
			if status == "completed" {
				var token string
				if solution, ok := result["solution"].(map[string]interface{}); ok {
					if text, ok := solution["text"].(string); ok && len(text) > 3 {
						token = text
					}
					if captchaIDs, ok := solution["captchaIds"].([]interface{}); ok && len(captchaIDs) > 0 {
						if id, ok := captchaIDs[0].(string); ok {
							return &SolveResponse{Token: token, CaptchaID: id, Solved: true}, nil
						}
					}
				}
				return &SolveResponse{Token: token, CaptchaID: "", Solved: true}, nil
			}

			if status == "ready" || status == "processing" {
				continue
			}

			return nil, fmt.Errorf("task failed with status: %v", status)
		}

		if code, ok := result["code"].(float64); ok && code == 0 {
			continue
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}

func (s *Solver) pollCapMonster(ctx context.Context, taskID string) (*SolveResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pollTick.C:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/getTaskResult", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		query := req.URL.Query()
		query.Add("key", s.apiKey)
		query.Add("task", taskID)
		query.Add("action", "getTaskResult")
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

		if status, ok := result["status"].(string); ok {
			if status == "ready" {
				var token string
				if response, ok := result["response"].(string); ok && len(response) > 3 {
					token = response
				}

				var captchaID string
				if id, ok := result["taskId"].(string); ok {
					captchaID = id
					return &SolveResponse{Token: token, CaptchaID: captchaID, Solved: true}, nil
				}
			}

			if status == "processing" {
				continue
			}

			return nil, fmt.Errorf("task failed with status: %v", status)
		}

		if code, ok := result["code"].(float64); ok && code == 0 {
			continue
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}

func detectCAPTCHAType(html string) CAPTCHAType {
	if strings.Contains(html, "recaptcha") || strings.Contains(html, "google.com/recaptcha") {
		return TypeRecaptchaV2
	}
	if strings.Contains(html, "hcaptcha") || strings.Contains(html, "hcaptcha.com") {
		return TypeHCaptcha
	}
	if strings.Contains(html, "cf-turnstile") || strings.Contains(html, "challenges.cloudflare.com") {
		return TypeTurnstile
	}
	return TypeImageCaptcha
}

func FindCAPTCHAElements(ctx context.Context) map[string]string {
	result := make(map[string]string)

	script := `
		(function() {
			var result = {};
			var el = document.querySelector('.g-recaptcha, div[data-sitekey], .h-captcha, .cf-turnstile');
			if (el) {
				result.sitekey = el.getAttribute('data-sitekey') || '';
				result.type = el.classList.contains('g-recaptcha') ? 'recaptcha' :
					el.classList.contains('h-captcha') ? 'hcaptcha' : 'turnstile';
			}
			return JSON.stringify(result);
		})()
	`
	var res string
	_ = chromedp.Run(ctx, chromedp.Evaluate(script, &res))
	_ = json.Unmarshal([]byte(res), &result)
	return result
}

func (s *Solver) InjectSolution(ctx context.Context, token string) error {
	safeToken, _ := json.Marshal(token)

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