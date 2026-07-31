package jsengine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// ExtractJSONFromPage evaluates the matching element's text as JSON and returns the parsed result.
func ExtractJSONFromPage(ctx context.Context, selector string) (json.RawMessage, error) {
	selJSON, err := json.Marshal(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal selector: %w", err)
	}
	script := fmt.Sprintf(`
		(function(selector) {
			try {
				const el = document.querySelector(selector);
				if (el) {
					const text = el.textContent || el.innerText;
					return JSON.parse(text);
				}
			} catch(e) {}
			return null;
		})(%s)
	`, string(selJSON))
	var result json.RawMessage
	err = chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	return result, err
}
