package jsengine

import (
	"context"

	"github.com/chromedp/chromedp"
)

const DetectFrameworkScript = `
		(function() {
			const detection = {
				framework: 'unknown',
				version: null,
				isHydrated: false,
				hasSkeleton: false,
				loadingState: 'loaded'
			};

			// Check for loading indicators
			const loaders = document.querySelectorAll('.loading, .spinner, .loader, [class*="loading"], [class*="spinner"]');
			detection.hasSkeleton = loaders.length > 0;

			// Check for skeleton screens
			const skeletons = document.querySelectorAll('[class*="skeleton"], [class*="placeholder"], [class*="shimmer"]');
			detection.hasSkeleton = detection.hasSkeleton || skeletons.length > 0;

			// Next.js
			if (window.__NEXT_DATA__) {
				detection.framework = 'nextjs';
				detection.isHydrated = true;
			}

			// Nuxt
			if (window.__NUXT__) {
				detection.framework = 'nuxt';
				detection.isHydrated = true;
			}

			// Angular
			const ngVersion = document.querySelector('[ng-version]');
			if (ngVersion) {
				detection.framework = 'angular';
				detection.version = ngVersion.getAttribute('ng-version');
				detection.isHydrated = true;
			}

			// React
			if (document.querySelector('[data-reactroot]') || document.querySelector('#__next')) {
				detection.framework = 'react';
				detection.isHydrated = !!document.querySelector('[data-reactroot]');
			}

			// Vue
			if (document.querySelector('[data-v-]') || window.__VUE__) {
				detection.framework = 'vue';
				detection.isHydrated = true;
			}

			// Svelte
			if (document.querySelector('[class*="svelte-"]')) {
				detection.framework = 'svelte';
				detection.isHydrated = true;
			}

			// Check loading state
			if (document.readyState === 'loading') {
				detection.loadingState = 'loading';
			} else if (document.readyState === 'interactive') {
				detection.loadingState = 'domcontentloaded';
			} else {
				detection.loadingState = 'loaded';
			}

			return detection;
		})()
	`

// FrameworkInfo describes a detected frontend framework and its version.
type FrameworkInfo struct {
	Framework string      `json:"framework"`
	Version   string      `json:"version,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// FrameworkDetection reports the detected framework, hydration, and loading state.
type FrameworkDetection struct {
	Framework    string `json:"framework"`
	Version      string `json:"version,omitempty"`
	IsHydrated   bool   `json:"isHydrated"`
	HasSkeleton  bool   `json:"hasSkeleton"`
	LoadingState string `json:"loadingState"`
}

// DetectFramework identifies the frontend framework and its hydration and loading state.
func DetectFramework(ctx context.Context) (*FrameworkDetection, error) {
	var result FrameworkDetection
	err := chromedp.Run(ctx, chromedp.Evaluate(DetectFrameworkScript, &result))
	return &result, err
}
