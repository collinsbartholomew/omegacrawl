package jsengine

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const ExtractStructuredDataScript = `
		(function() {
			var result = { jsonld: [], og: {}, twitter: {}, meta: {} };
			document.querySelectorAll('script[type="application/ld+json"]').forEach(function(el) {
				try {
					result.jsonld.push(JSON.parse(el.textContent));
				} catch(e) {}
			});
			document.querySelectorAll('meta[property^="og:"]').forEach(function(el) {
				var key = el.getAttribute('property').replace('og:', '');
				result.og[key] = el.getAttribute('content') || '';
			});
			document.querySelectorAll('meta[name^="twitter:"]').forEach(function(el) {
				var key = el.getAttribute('name').replace('twitter:', '');
				result.twitter[key] = el.getAttribute('content') || '';
			});
			document.querySelectorAll('meta[name], meta[property]').forEach(function(el) {
				var name = el.getAttribute('name') || el.getAttribute('property') || '';
				if (!name.startsWith('og:') && !name.startsWith('twitter:')) {
					result.meta[name] = el.getAttribute('content') || '';
				}
			});
			return JSON.stringify(result);
		})()
	`

// StructuredData holds JSON-LD, Open Graph, Twitter, and generic meta tag data.
type StructuredData struct {
	JSONLD  []interface{}     `json:"jsonld"`
	OG      map[string]string `json:"og"`
	Twitter map[string]string `json:"twitter"`
	Meta    map[string]string `json:"meta"`
}

// ExtractStructuredData extracts JSON-LD, Open Graph, Twitter, and meta tag data from the page.
func ExtractStructuredData(ctx context.Context) (*StructuredData, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractStructuredDataScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return &StructuredData{}, nil
	}
	var data StructuredData
	if err := json.Unmarshal([]byte(resultJSON), &data); err != nil {
		return nil, err
	}
	if data.OG == nil {
		data.OG = make(map[string]string)
	}
	if data.Twitter == nil {
		data.Twitter = make(map[string]string)
	}
	if data.Meta == nil {
		data.Meta = make(map[string]string)
	}
	return &data, nil
}
