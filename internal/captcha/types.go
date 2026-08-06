package captcha

import (
	"net/http"
	"time"
)

// Provider identifies a CAPTCHA solving service.
type Provider string

// CAPTCHAType identifies the type of CAPTCHA to solve.
type CAPTCHAType string

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
