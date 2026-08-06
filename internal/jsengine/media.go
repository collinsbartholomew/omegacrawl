package jsengine

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const ExtractIframeSourcesScript = `
		(function() {
			const iframes = document.querySelectorAll('iframe, frame');
			const results = [];
			const seen = new Set();
			iframes.forEach(iframe => {
				let src = iframe.getAttribute('src');
				if (!src) {
					src = iframe.getAttribute('data-src') || iframe.getAttribute('data-lazy-src');
				}
				if (src && !seen.has(src)) {
					seen.add(src);
					results.push({
						src: src,
						width: iframe.getAttribute('width') || '',
						height: iframe.getAttribute('height') || '',
						id: iframe.id || '',
						sandbox: iframe.getAttribute('sandbox') || ''
					});
				}
			});
			return JSON.stringify(results);
		})()
	`

const ExtractMediaSourcesScript = `
		(function() {
			const results = [];
			const seen = new Set();
			function addSource(src, type, el) {
				if (src && !seen.has(src)) {
					seen.add(src);
					results.push({
						src: src,
						type: type,
						poster: el.getAttribute('poster') || '',
						width: el.getAttribute('width') || '',
						height: el.getAttribute('height') || '',
						id: el.id || ''
					});
				}
			}
			document.querySelectorAll('video, audio').forEach(media => {
				const src = media.getAttribute('src');
				if (src) addSource(src, media.tagName.toLowerCase(), media);
				media.querySelectorAll('source').forEach(source => {
					const s = source.getAttribute('src');
					if (s) addSource(s, media.tagName.toLowerCase() + '-source', source);
				});
			});
			document.querySelectorAll('source').forEach(source => {
				const parent = source.parentElement;
				if (parent && parent.tagName !== 'VIDEO' && parent.tagName !== 'AUDIO') {
					const s = source.getAttribute('src');
					if (s) addSource(s, 'source', source);
				}
			});
			return JSON.stringify(results);
		})()
	`

// IframeSource describes a discovered iframe or frame element.
type IframeSource struct {
	Src     string `json:"src"`
	Width   string `json:"width"`
	Height  string `json:"height"`
	ID      string `json:"id"`
	Sandbox string `json:"sandbox"`
}

// MediaSource describes a discovered video or audio source.
type MediaSource struct {
	Src    string `json:"src"`
	Type   string `json:"type"`
	Poster string `json:"poster"`
	Width  string `json:"width"`
	Height string `json:"height"`
	ID     string `json:"id"`
}

// ExtractIframeSources extracts the source attributes of all iframes and frames on the page.
func ExtractIframeSources(ctx context.Context) ([]IframeSource, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractIframeSourcesScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return []IframeSource{}, nil
	}
	var iframes []IframeSource
	if err := json.Unmarshal([]byte(resultJSON), &iframes); err != nil {
		return nil, err
	}
	return iframes, nil
}

// ExtractMediaSources extracts the source attributes of all video and audio elements on the page.
func ExtractMediaSources(ctx context.Context) ([]MediaSource, error) {
	var resultJSON string
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractMediaSourcesScript, &resultJSON))
	if err != nil {
		return nil, err
	}
	if resultJSON == "" || resultJSON == "null" {
		return []MediaSource{}, nil
	}
	var media []MediaSource
	if err := json.Unmarshal([]byte(resultJSON), &media); err != nil {
		return nil, err
	}
	return media, nil
}
