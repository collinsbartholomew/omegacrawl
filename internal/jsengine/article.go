package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const ExtractArticleScript = `
		(function() {
			function getArticleContent() {
				// Try readability-style extraction
				const article = document.querySelector('article');
				if (article) return article.outerHTML;

				// Try common content selectors
				const selectors = [
					'[role="main"]',
					'.main-content',
					'.content',
					'#content',
					'.post-content',
					'.entry-content',
					'.article-body',
					'[itemprop="articleBody"]'
				];
				for (const sel of selectors) {
					const el = document.querySelector(sel);
					if (el) return el.outerHTML;
				}

				// Fallback: largest text container
				const candidates = document.querySelectorAll('div, section, main');
				let best = null;
				let maxText = 0;
				candidates.forEach(el => {
					const text = el.innerText || '';
					if (text.length > maxText) {
						maxText = text.length;
						best = el;
					}
				});
				return best ? best.outerHTML : document.body.innerHTML;
			}

			return {
				title: document.title,
				content: getArticleContent(),
				url: window.location.href,
				extractedAt: new Date().toISOString()
			};
		})()
	`

// ArticleContent holds the title, content, URL, and extraction timestamp of an extracted article.
type ArticleContent struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	URL         string `json:"url"`
	ExtractedAt string `json:"extracted_at"`
}

// ExtractArticle extracts the main article content from the page.
func ExtractArticle(ctx context.Context) (*ArticleContent, error) {
	var result ArticleContent
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractArticleScript, &result))
	return &result, err
}
