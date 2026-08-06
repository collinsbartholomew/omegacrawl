package crawler

import (
	"context"
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
						return '//*[@id=' + escapeXPathLiteral(element.id) + ']';
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

				function escapeXPathLiteral(value) {
					value = String(value);
					if (value.indexOf("'") === -1) {
						return "'" + value + "'";
					}
					if (value.indexOf('"') === -1) {
						return '"' + value + '"';
					}
					return "concat('" + value.split("'").join("', \"'\", '") + "')";
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

const cookieJarFile = "cookies.json"
