package crawler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/jsengine"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) runInteractionEngine(ctx context.Context, urlStr string) {
	if !c.cfg.EnableInteractionEngine {
		return
	}

	maxInteractions := c.cfg.MaxInteractionsPerPage
	if maxInteractions <= 0 {
		maxInteractions = 50
	}

	interactionCount := 0

	interactedElements := make(map[string]bool)

	for interactionCount < maxInteractions {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		script := `
			(function() {
				var results = [];
				var selectors = [
					'button:not([disabled]):not([type="submit"]):not([type="reset"])',
					'a[href]:not([href^="mailto:"]):not([href^="tel:"]):not([href^="javascript:"]):not([target="_blank"])',
					'input[type="button"]:not([disabled])',
					'input[type="submit"]:not([disabled])',
					'[role="button"]:not([aria-disabled="true"])',
					'[onclick]',
					'.btn:not([disabled])',
					'button.btn:not([disabled])',
					'[data-action]',
					'[data-click]',
					'[data-toggle]',
					'[data-dismiss]',
					'summary',
					'details:not([open]) > summary',
					'.accordion-header',
					'.accordion-trigger',
					'[aria-expanded="false"]',
					'.collapsible:not(.active)',
					'[data-bs-toggle="collapse"]',
					'[data-bs-toggle="modal"]',
					'[data-toggle="tab"]',
					'[data-toggle="pill"]'
				];

				for (var i = 0; i < selectors.length; i++) {
					var elements = document.querySelectorAll(selectors[i]);
					for (var j = 0; j < elements.length; j++) {
						var el = elements[j];
						if (!el.offsetParent && el.tagName !== 'SUMMARY') continue;
						var rect = el.getBoundingClientRect();
						if (rect.width === 0 && rect.height === 0) continue;
						var xpath = getXPath(el);
						if (xpath) {
							results.push({
								xpath: xpath,
								tag: el.tagName.toLowerCase(),
								text: (el.textContent || '').trim().substring(0, 100),
								href: el.href || '',
								type: 'click'
							});
						}
					}
				}

				var forms = document.querySelectorAll('form');
				for (var i = 0; i < forms.length; i++) {
					var form = forms[i];
					var action = form.action || '';
					var method = (form.method || 'GET').toUpperCase();
					if (action && action.indexOf('javascript:') === -1) {
						var xpath = getXPath(form);
						if (xpath) {
							results.push({
								xpath: xpath,
								tag: 'form',
								action: action,
								method: method,
								type: 'form'
							});
						}
					}
				}

				var inputs = document.querySelectorAll('input[type="text"], input[type="email"], input[type="password"], input[type="search"], textarea, select');
				for (var i = 0; i < inputs.length; i++) {
					var input = inputs[i];
					if (!input.offsetParent) continue;
					var xpath = getXPath(input);
					if (xpath) {
						results.push({
							xpath: xpath,
							tag: input.tagName.toLowerCase(),
							type: input.type || '',
							name: input.name || '',
							placeholder: input.placeholder || '',
							type: 'fill'
						});
					}
				}

				return results;

				function getXPath(element) {
					if (element.id !== '') {
						return 'id("' + element.id + '")';
					}
					if (element === document.body) {
						return '/html/body';
					}
					var ix = 0;
					var siblings = element.parentNode.childNodes;
					for (var i = 0; i < siblings.length; i++) {
						var sibling = siblings[i];
						if (sibling === element) {
							return getXPath(element.parentNode) + '/' + element.tagName.toLowerCase() + '[' + (ix + 1) + ']';
						}
						if (sibling.nodeType === 1 && sibling.tagName === element.tagName) {
							ix++;
						}
					}
					return null;
				}
			})()
		`

		var result []map[string]interface{}
		err := chromedp.Run(ctx, chromedp.Evaluate(script, &result))
		if err != nil {
			util.LogDebug("interaction discovery failed", zap.Error(err))
			break
		}

		if len(result) == 0 {
			break
		}

		foundNew := false
		for _, item := range result {
			if interactionCount >= maxInteractions {
				break
			}

			xpath := ""
			if x, ok := item["xpath"].(string); ok {
				xpath = x
			}

			if xpath == "" || interactedElements[xpath] {
				continue
			}

			itemType := ""
			if t, ok := item["type"].(string); ok {
				itemType = t
			}

			handled := false

			switch itemType {
			case "click":
				handled = c.clickElement(ctx, xpath, item)
			case "form":
				handled = c.interactWithForm(ctx, xpath, item)
			case "fill":
				handled = c.fillInput(ctx, xpath, item)
			}

			if handled {
				interactedElements[xpath] = true
				interactionCount++
				foundNew = true

				waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				jsengine.WaitForNetworkIdle(waitCtx, 1*time.Second)
				cancel()

				time.Sleep(500 * time.Millisecond)
			}
		}

		if !foundNew {
			break
		}
	}

	if c.cfg.EnableLazyLoad {
		jsengine.InjectLazyLoad(ctx)
	}

	scrollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	jsengine.InfiniteScroll(scrollCtx, &jsengine.InfiniteScrollConfig{
		Enabled:          true,
		MaxScrolls:       10,
		MaxDuration:      10 * time.Second,
		StablePasses:     2,
		ItemSelector:     "article, .card, .list-item, [data-infinite-scroll-item], .feed-item",
		ScrollContainer:  "",
		LoadMoreSelector: "",
		ScrollDelay:      1 * time.Second,
		ScrollDistance:   500,
	})
	cancel()
}

func (c *Crawler) clickElement(ctx context.Context, xpath string, item map[string]interface{}) bool {
	script := fmt.Sprintf(`
		(function(xpath) {
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
	`, xpath)

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

	// Fill all input/select/textarea elements inside the form
	script := fmt.Sprintf(`
		(function(xpath) {
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
	`, xpath)

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
		(function(xpath, value) {
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
	`, xpath, value)

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

const cookieJarFile = "cookies.json"
