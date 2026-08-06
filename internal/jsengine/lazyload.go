package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const LazyLoadScript = `
		// Trigger lazy loading for all images
		document.querySelectorAll('img[data-src], img[data-lazy-src], img[loading="lazy"]').forEach(img => {
			if (img.dataset.src) img.src = img.dataset.src;
			if (img.dataset.lazySrc) img.src = img.dataset.lazySrc;
		});

		// Trigger lazy loading for background images
		document.querySelectorAll('[data-bg], [data-background]').forEach(el => {
			const bg = el.dataset.bg || el.dataset.background;
			if (bg) el.style.backgroundImage = 'url(' + bg + ')';
		});

		// Trigger lazy loading for iframes
		document.querySelectorAll('iframe[data-src]').forEach(iframe => {
			if (iframe.dataset.src) iframe.src = iframe.dataset.src;
		});
	`

// InjectLazyLoad injects the script that triggers lazy-loaded resources.
func InjectLazyLoad(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate(LazyLoadScript, nil))
}
