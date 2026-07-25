package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

type ServiceWorkerInfo struct {
	Count         int
	Registrations []ServiceWorkerRegistration
}

type ServiceWorkerRegistration struct {
	Scope  string
	Active *string
}

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


