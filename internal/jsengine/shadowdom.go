package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const ExtractShadowDOMScript = `
		(function() {
			function deepQuery(root, selector) {
				const results = [];
				const elements = root.querySelectorAll(selector);
				elements.forEach(el => results.push(el.outerHTML));

				// Penetrate shadow DOM
				root.querySelectorAll('*').forEach(el => {
					if (el.shadowRoot) {
						results.push(...deepQuery(el.shadowRoot, selector));
					}
				});

				return results;
			}

			// Get all shadow roots
			const shadowRoots = [];
			document.querySelectorAll('*').forEach(el => {
				if (el.shadowRoot) {
					shadowRoots.push({
						tag: el.tagName.toLowerCase(),
						innerHTML: el.shadowRoot.innerHTML
					});
				}
			});

			return {
				shadowRoots: shadowRoots,
				count: shadowRoots.length
			};
		})()
	`

// ShadowDOMInfo lists all shadow roots found on a page.
type ShadowDOMInfo struct {
	ShadowRoots []ShadowRoot `json:"shadowRoots"`
	Count       int          `json:"count"`
}

// ShadowRoot describes a single shadow root's host tag and inner HTML.
type ShadowRoot struct {
	Tag       string `json:"tag"`
	InnerHTML string `json:"innerHTML"`
}

// ExtractShadowDOM collects all shadow roots and their inner HTML from the page.
func ExtractShadowDOM(ctx context.Context) (*ShadowDOMInfo, error) {
	var result ShadowDOMInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractShadowDOMScript, &result))
	return &result, err
}
