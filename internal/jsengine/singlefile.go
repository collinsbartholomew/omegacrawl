package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const SingleFileScript = `
		(async function() {
			const tasks = [];

			// Inline CSS
			document.querySelectorAll('link[rel="stylesheet"]').forEach(link => {
				const href = link.href;
				if (!href) return;
				tasks.push(
					fetch(href).then(r => r.text()).then(css => {
						const style = document.createElement('style');
						style.textContent = css;
						link.parentNode.replaceChild(style, link);
					}).catch(() => {})
				);
			});

			// Inline scripts
			document.querySelectorAll('script[src]').forEach(script => {
				const src = script.src;
				if (!src) return;
				tasks.push(
					fetch(src).then(r => r.text()).then(js => {
						const inline = document.createElement('script');
						inline.textContent = js;
						script.parentNode.replaceChild(inline, script);
					}).catch(() => {})
				);
			});

			// Inline images as data URLs
			document.querySelectorAll('img[src]').forEach(img => {
				const src = img.src;
				if (!src || src.startsWith('data:')) return;
				tasks.push(
					fetch(src).then(r => r.blob()).then(blob => {
						return new Promise((resolve) => {
							const reader = new FileReader();
							reader.onloadend = () => {
								img.src = reader.result;
								resolve();
							};
							reader.readAsDataURL(blob);
						});
					}).catch(() => {})
				);
			});

			await Promise.all(tasks);
			return document.documentElement.outerHTML;
		})()
	`

// GenerateSingleFile inlines CSS, scripts, and images, returning the page as a single HTML file.
func GenerateSingleFile(ctx context.Context) (string, error) {
	var result string
	err := chromedp.Run(ctx, chromedp.Evaluate(SingleFileScript, &result))
	return result, err
}
