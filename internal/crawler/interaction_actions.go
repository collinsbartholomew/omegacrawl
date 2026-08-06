package crawler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// encodeJSArg base64-encodes a string so it can be passed safely into an
// evaluated script without risking quote/backtick injection from untrusted
// page-derived values (XPaths built from element ids/attributes).
func encodeJSArg(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// decodeJSArg must mirror encodeJSArg inside evaluated scripts.
const decodeJSArgJS = `function(enc){return atob(enc);}`

func (c *Crawler) clickElement(ctx context.Context, xpath string, item map[string]interface{}) bool {
	script := fmt.Sprintf(`
		(function(enc) {
			var xpath = atob(enc);
			var result = document.evaluate(xpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
			var el = result.singleNodeValue;
			if (!el) return { success: false, reason: 'not found' };
			if (el.tagName === 'A' && el.href) {
				var url = el.href;
				if (url.startsWith('http') && !url.startsWith(window.location.origin)) {
					return { success: false, reason: 'external link' };
				}
			}
			try {
				el.click();
				return { success: true, tag: el.tagName };
			} catch(e) {
				return { success: false, reason: e.message };
			}
		})("%s")
	`, encodeJSArg(xpath))

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false
	}

	if success, ok := result["success"].(bool); ok && success {
		tag := ""
		if t, ok := result["tag"].(string); ok {
			tag = t
		}
		util.LogDebug("interaction: clicked element", zap.String("xpath", xpath), zap.String("tag", tag))
		return true
	}
	return false
}

func (c *Crawler) interactWithForm(ctx context.Context, xpath string, item map[string]interface{}) bool {
	action := ""
	if a, ok := item["action"].(string); ok {
		action = a
	}
	method := ""
	if m, ok := item["method"].(string); ok {
		method = m
	}

	if action == "" || strings.HasPrefix(action, "javascript:") {
		return false
	}

	util.LogDebug("interaction: submitting form", zap.String("xpath", xpath), zap.String("action", action), zap.String("method", method))

	script := fmt.Sprintf(`
		(function(enc) {
			var xpath = atob(enc);
			var result = document.evaluate(xpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
			var form = result.singleNodeValue;
			if (!form || !form.elements) return { success: false, reason: 'not found or not a form' };

			// Fill inputs with sensible defaults
			for (var i = 0; i < form.elements.length; i++) {
				var el = form.elements[i];
				if (el.disabled || el.readOnly) continue;
				var type = (el.type || '').toLowerCase();

				if (type === 'text' || type === 'search' || type === 'tel' || type === 'url') {
					el.focus();
					if (el.name && el.name.toLowerCase().indexOf('search') >= 0) {
						el.value = 'test query';
					} else if (el.name && (el.name.toLowerCase().indexOf('name') >= 0 || el.placeholder && el.placeholder.toLowerCase().indexOf('name') >= 0)) {
						el.value = 'Test User';
					} else if (el.name && el.name.toLowerCase().indexOf('email') >= 0) {
						el.value = 'test@example.com';
					} else if (el.name && (el.name.toLowerCase().indexOf('phone') >= 0 || el.name.toLowerCase().indexOf('tel') >= 0)) {
						el.value = '+1-555-555-0100';
					} else {
						el.value = 'test';
					}
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
				} else if (type === 'email') {
					el.focus();
					el.value = 'test@example.com';
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
				} else if (type === 'password') {
					el.focus();
					el.value = 'testpassword123';
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
				} else if (type === 'checkbox' || type === 'radio') {
					el.checked = true;
					el.dispatchEvent(new Event('change', { bubbles: true }));
				} else if (type === 'select-one' || type === 'select-multiple') {
					if (el.options.length > 0) {
						for (var j = 0; j < el.options.length; j++) {
							if (!el.options[j].disabled) {
								el.selectedIndex = j;
								el.dispatchEvent(new Event('change', { bubbles: true }));
								break;
							}
						}
					}
				} else if (type === 'textarea') {
					el.focus();
					el.value = 'test content for textarea';
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
				}
			}

			// Submit the form
			try {
				var submitBtn = form.querySelector('input[type="submit"], button[type="submit"], button:not([type])');
				if (submitBtn) {
					submitBtn.click();
				} else {
					form.submit();
				}
				return { success: true };
			} catch(e) {
				return { success: false, reason: e.message };
			}
		})("%s")
	`, encodeJSArg(xpath))

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false
	}

	if success, ok := result["success"].(bool); ok && success {
		util.LogDebug("interaction: form submitted", zap.String("xpath", xpath))
		return true
	}
	return false
}

func (c *Crawler) fillInput(ctx context.Context, xpath string, item map[string]interface{}) bool {
	inputType := ""
	if t, ok := item["type"].(string); ok {
		inputType = t
	}
	name := ""
	if n, ok := item["name"].(string); ok {
		name = n
	}
	placeholder := ""
	if p, ok := item["placeholder"].(string); ok {
		placeholder = p
	}

	value := "test"
	if inputType == "email" {
		value = "test@example.com"
	} else if inputType == "password" {
		value = "testpassword123"
	} else if strings.Contains(strings.ToLower(name), "search") || strings.Contains(strings.ToLower(placeholder), "search") {
		value = "test query"
	}

	script := fmt.Sprintf(`
		(function(encX, encV) {
			var xpath = atob(encX);
			var value = atob(encV);
			var result = document.evaluate(xpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
			var el = result.singleNodeValue;
			if (!el) return { success: false, reason: 'not found' };
			try {
				el.focus();
				el.value = value;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return { success: true };
			} catch(e) {
				return { success: false, reason: e.message };
			}
		})("%s", "%s")
	`, encodeJSArg(xpath), encodeJSArg(value))

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
	if err != nil {
		return false
	}

	if success, ok := result["success"].(bool); ok && success {
		util.LogDebug("interaction: filled input", zap.String("xpath", xpath), zap.String("name", name))
		return true
	}
	return false
}
