package captcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/clone/internal/config"
)

func TestExtractSolution2Captcha(t *testing.T) {
	token, id := extractSolution(map[string]interface{}{
		"solution": map[string]interface{}{
			"text":       "CAPTCHA_TOKEN",
			"captchaIds": []interface{}{"cap_123"},
		},
	})
	if token != "CAPTCHA_TOKEN" {
		t.Fatalf("expected token CAPTCHA_TOKEN, got %q", token)
	}
	if id != "cap_123" {
		t.Fatalf("expected captcha id cap_123, got %q", id)
	}
}

func TestExtractSolutionAntiCaptcha(t *testing.T) {
	token, _ := extractSolution(map[string]interface{}{
		"solution": map[string]interface{}{
			"gRecaptchaResponse": "ANTI_TOKEN",
		},
	})
	if token != "ANTI_TOKEN" {
		t.Fatalf("expected token ANTI_TOKEN, got %q", token)
	}
}

func TestExtractSolutionRequestField(t *testing.T) {
	token, _ := extractSolution(map[string]interface{}{
		"request": "REQ_TOKEN",
	})
	if token != "REQ_TOKEN" {
		t.Fatalf("expected token REQ_TOKEN, got %q", token)
	}
}

func TestIs2CaptchaError(t *testing.T) {
	cases := map[string]bool{
		"ERROR_CAPTCHA_UNSOLVABLE": true,
		"CAPCHA_NOT_READY":         true,
		"03AHjFGhTknP...":          false,
		"short":                    false,
	}
	for s, want := range cases {
		if got := is2CaptchaError(s); got != want {
			t.Errorf("is2CaptchaError(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestNewSolverDefaultsZeroTimeout(t *testing.T) {
	s := NewSolver(&config.CAPTCHAConfig{
		Enabled:    true,
		Provider:   "2captcha",
		APIKey:     "key",
		Timeout:    0,
		RetryCount: -3,
	})
	if s == nil {
		t.Fatal("expected non-nil solver")
	}
	if s.timeout <= 0 {
		t.Errorf("expected default timeout > 0, got %v", s.timeout)
	}
	if s.retryCnt != 0 {
		t.Errorf("expected retryCnt clamped to 0, got %d", s.retryCnt)
	}
}

func TestPoll2CaptchaRejectsErrorString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ERROR_CAPTCHA_UNSOLVABLE"))
	}))
	defer srv.Close()

	s := &Solver{
		provider: Provider2Captcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		client:   srv.Client(),
	}

	resp, err := s.poll2Captcha(context.Background(), "123")
	if err == nil {
		t.Fatalf("expected error for unsolvable captcha, got response: %+v", resp)
	}
}

func TestPoll2CaptchaSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/res.php" {
			t.Errorf("expected GET /res.php, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("action"); got != "get" {
			t.Errorf("expected action=get, got %q", got)
		}
		if got := r.URL.Query().Get("id"); got != "123" {
			t.Errorf("expected id=123, got %q", got)
		}
		w.Write([]byte("OK|VALID_TOKEN"))
	}))
	defer srv.Close()

	s := &Solver{
		provider: Provider2Captcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		client:   srv.Client(),
	}

	resp, err := s.poll2Captcha(context.Background(), "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Token != "VALID_TOKEN" || !resp.Solved {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestPoll2CaptchaNotReadyRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Write([]byte("CAPCHA_NOT_READY"))
			return
		}
		w.Write([]byte("OK|TOKEN_AFTER_RETRY"))
	}))
	defer srv.Close()

	s := &Solver{
		provider: Provider2Captcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		client:   srv.Client(),
	}

	resp, err := s.poll2Captcha(context.Background(), "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Token != "TOKEN_AFTER_RETRY" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected at least 2 poll calls, got %d", calls)
	}
}

func TestSubmitTask2CaptchaHCaptchaUsesSitekey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if got := r.Form.Get("method"); got != "hcaptcha" {
			t.Errorf("expected method=hcaptcha, got %q", got)
		}
		if got := r.Form.Get("sitekey"); got != "HCAPTCHA_KEY" {
			t.Errorf("expected sitekey=HCAPTCHA_KEY, got %q", got)
		}
		if got := r.Form.Get("googlekey"); got != "" {
			t.Errorf("expected no googlekey for hcaptcha, got %q", got)
		}
		w.Write([]byte("OK|555"))
	}))
	defer srv.Close()

	s := &Solver{
		provider: Provider2Captcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		retryCnt: 0,
		client:   srv.Client(),
	}

	id, err := s.submitTask(context.Background(), map[string]interface{}{
		"type":       "HCaptchaTask",
		"websiteURL": "https://example.com",
		"websiteKey": "HCAPTCHA_KEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "555" {
		t.Errorf("expected task id 555, got %q", id)
	}
}

func TestSubmitTask2CaptchaLegacyForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/in.php" {
			t.Errorf("expected POST /in.php, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content type, got %q", ct)
		}
		r.ParseForm()
		if got := r.Form.Get("key"); got != "key" {
			t.Errorf("expected key=key, got %q", got)
		}
		if got := r.Form.Get("method"); got != "userrecaptcha" {
			t.Errorf("expected method=userrecaptcha, got %q", got)
		}
		if got := r.Form.Get("googlekey"); got != "SITE_KEY" {
			t.Errorf("expected googlekey=SITE_KEY, got %q", got)
		}
		if got := r.Form.Get("pageurl"); got != "https://example.com" {
			t.Errorf("expected pageurl, got %q", got)
		}
		w.Write([]byte("OK|987654"))
	}))
	defer srv.Close()

	s := &Solver{
		provider: Provider2Captcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		retryCnt: 0,
		client:   srv.Client(),
	}

	id, err := s.submitTask(context.Background(), map[string]interface{}{
		"type":       "RecaptchaV2Task",
		"websiteURL": "https://example.com",
		"websiteKey": "SITE_KEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "987654" {
		t.Errorf("expected task id 987654, got %q", id)
	}
}

func TestPollAntiCaptchaRejectsEmptySolution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
		})
	}))
	defer srv.Close()

	s := &Solver{
		provider: ProviderAntiCaptcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		client:   srv.Client(),
	}

	resp, err := s.pollAntiCaptcha(context.Background(), "456")
	if err == nil {
		t.Fatalf("expected error for ready-without-solution, got: %+v", resp)
	}
}

func TestCreateTaskAndPollRetriesSubmit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := atomic.AddInt32(&calls, 1); n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"taskId": 7,
		})
	}))
	defer srv.Close()

	s := &Solver{
		provider: ProviderAntiCaptcha,
		apiKey:   "key",
		baseURL:  srv.URL,
		timeout:  5 * time.Second,
		retryCnt: 2,
		client:   srv.Client(),
	}

	// Force poll to return immediately after submit to avoid long polling.
	// Use a poll fn that returns a canned result.
	resp, err := s.createTaskAndPoll(context.Background(), map[string]interface{}{"type": "HCaptchaTask"}, func(ctx context.Context, id string) (*SolveResponse, error) {
		if id != "7" {
			t.Errorf("expected taskID 7, got %q", id)
		}
		return &SolveResponse{Token: "tok", Solved: true}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Token != "tok" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 createTask calls, got %d", calls)
	}
}
