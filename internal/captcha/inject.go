package captcha

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

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
