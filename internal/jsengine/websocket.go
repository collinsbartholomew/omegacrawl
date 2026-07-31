package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

// ServiceWorkerInfo summarizes the service worker registrations found in a page.
type ServiceWorkerInfo struct {
	Count         int
	Registrations []ServiceWorkerRegistration
}

// ServiceWorkerRegistration describes a single service worker registration.
type ServiceWorkerRegistration struct {
	Scope  string
	Active *string
}

// DetectServiceWorkers queries the page for all service worker registrations.
func DetectServiceWorkers(ctx context.Context) (*ServiceWorkerInfo, error) {
	script := `
		(async () => {
			if (!('serviceWorker' in navigator)) return { count: 0, registrations: [] };
			const regs = await navigator.serviceWorker.getRegistrations();
			return {
				count: regs.length,
				registrations: regs.map(r => ({
					scope: r.scope,
					active: r.active ? r.active.scriptURL : null
				}))
			};
		})()
	`
	var info ServiceWorkerInfo
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &info))
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// UnregisterServiceWorkers unregisters all service workers in the page and returns the count removed.
func UnregisterServiceWorkers(ctx context.Context) (int, error) {
	script := `
		(async () => {
			if (!('serviceWorker' in navigator)) return 0;
			const regs = await navigator.serviceWorker.getRegistrations();
			let count = 0;
			for (const reg of regs) {
				await reg.unregister();
				count++;
			}
			return count;
		})()
	`
	var count int
	err := chromedp.Run(ctx, chromedp.Evaluate(script, &count))
	return count, err
}
