package jsengine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

func ExtractJSONFromPage(ctx context.Context, selector string) (json.RawMessage, error) {
	selJSON, _ := json.Marshal(selector)
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
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	return result, err
}
