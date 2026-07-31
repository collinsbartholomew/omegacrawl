package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) promptUser(tabCtx context.Context, urlStr string) {
	fmt.Printf("\n=== Interactive Mode ===\n")
	fmt.Printf("Page: %s\n", urlStr)
	fmt.Print("Press Enter after handling any challenges in the browser (or 'q' to quit): ")
	var input string
	_, err := fmt.Scanln(&input)
	if err != nil {
		util.LogDebug("interactive prompt error", zap.Error(err))
		return
	}
	if input == "q" || input == "Q" || input == "quit" || input == "exit" {
		util.LogInfo("user requested stop in interactive mode")
		c.Stop()
	}
}

func (c *Crawler) solveCaptcha(tabCtx context.Context, urlStr string, html string) {
	if c.captchaSolver == nil || c.cfg.CAPTCHAConfig == nil || !c.cfg.CAPTCHAConfig.Enabled {
		return
	}
	if c.cfg.CAPTCHAConfig.Provider == "" {
		return
	}
	solved := false
	for attempt := 0; attempt < c.cfg.CAPTCHAConfig.RetryCount; attempt++ {
		if attempt > 0 {
			captchaTimer := time.NewTimer(time.Duration(attempt*2) * time.Second)
			select {
			case <-captchaTimer.C:
			case <-c.ctx.Done():
				if !captchaTimer.Stop() {
					<-captchaTimer.C
				}
				return
			}
		}
		elems, exists := c.detectCaptchaElements(tabCtx, urlStr)
		if !exists {
			return
		}
		req := captcha.SolveRequest{
			URL:      urlStr,
			SiteKey:  elems["sitekey"],
			PageHTML: html,
		}
		if req.SiteKey == "" {
			return
		}
		resp, err := c.captchaSolver.Solve(tabCtx, req)
		if err != nil {
			util.LogDebug("captcha solve failed",
				zap.String("url", urlStr),
				zap.Error(err),
				zap.Int("attempt", attempt+1),
			)
			continue
		}
		if resp.Solved && resp.Token != "" {
			c.injectCaptchaToken(tabCtx, resp.Token, elems)
			solved = true
			postSolveTimer := time.NewTimer(2 * time.Second)
			select {
			case <-postSolveTimer.C:
			case <-c.ctx.Done():
				if !postSolveTimer.Stop() {
					<-postSolveTimer.C
				}
				return
			}
			break
		}
	}
	if !solved {
		util.LogDebug("captcha not solved after retries", zap.String("url", urlStr))
	}
}

func (c *Crawler) detectCaptchaElements(tabCtx context.Context, urlStr string) (map[string]string, bool) {
	result := make(map[string]string)
	var scriptResult string
	script := `
		(function() {
			var result = {};

			// reCAPTCHA v2/v3
			var recaptcha = document.querySelector('.g-recaptcha, div[data-sitekey], [data-runtime="google/recaptcha"], iframe[src*="google.com/recaptcha"], .recaptcha');
			if (recaptcha) {
				result["sitekey"] = recaptcha.getAttribute("data-sitekey") || recaptcha.dataset.sitekey || "";
				result["type"] = "recaptcha";
				result["found"] = true;
			}

			// hCaptcha
			var hcaptcha = document.querySelector('.h-captcha, iframe[src*="hcaptcha.com"], div[data-sitekey][data-theme]');
			if (hcaptcha && !result.found) {
				result["sitekey"] = hcaptcha.getAttribute("data-sitekey") || "";
				result["type"] = "hcaptcha";
				result["found"] = true;
			}

			// Cloudflare Turnstile
			var turnstile = document.querySelector('.cf-turnstile, div[data-sitekey][data-appearance], iframe[src*="challenges.cloudflare.com"]');
			if (turnstile && !result.found) {
				result["sitekey"] = turnstile.getAttribute("data-sitekey") || "";
				result["type"] = "turnstile";
				result["found"] = true;
			}

			// Generic fallback: any element with data-sitekey attribute
			if (!result.found) {
				var allSiteKeys = document.querySelectorAll('[data-sitekey]');
				if (allSiteKeys.length > 0) {
					result["sitekey"] = allSiteKeys[0].getAttribute("data-sitekey") || "";
					result["type"] = "generic";
					result["found"] = true;
				}
			}

			// Check for loaded CAPTCHA scripts
			if (!result.found) {
				var scripts = document.querySelectorAll('script[src*="recaptcha"], script[src*="hcaptcha"], script[src*="challenges.cloudflare"]');
				if (scripts.length > 0) {
					result["type"] = "detected_via_script";
					result["found"] = true;
				}
			}

			return JSON.stringify(result);
		})()
	`
	err := chromedp.Run(tabCtx, chromedp.Evaluate(script, &scriptResult))
	if err != nil {
		return nil, false
	}
	if err := json.Unmarshal([]byte(scriptResult), &result); err != nil {
		return nil, false
	}
	siteKey, _ := result["sitekey"]
	if siteKey == "" {
		return nil, false
	}
	return result, true
}

func (c *Crawler) injectCaptchaToken(tabCtx context.Context, token string, elems map[string]string) {
	safeToken, _ := json.Marshal(token)
	tokenStr := string(safeToken)
	captchaType := elems["type"]
	switch captchaType {
	case "hcaptcha":
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector('[data-hcaptcha-response]').innerHTML = %s;`, tokenStr), nil)); err != nil {
			util.LogDebug("failed to inject hcaptcha token", zap.Error(err))
		}
	case "turnstile":
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector('[data-turnstile-response]').innerHTML = %s;`, tokenStr), nil)); err != nil {
			util.LogDebug("failed to inject turnstile token", zap.Error(err))
		}
	default:
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(
			fmt.Sprintf(`var e = document.getElementById("g-recaptcha-response"); if(e) e.innerHTML = %s;`, tokenStr), nil)); err != nil {
			util.LogDebug("failed to inject recaptcha token", zap.Error(err))
		}
	}
}
