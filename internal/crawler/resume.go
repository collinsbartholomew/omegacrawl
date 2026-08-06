package crawler

import (
	"regexp"
)

var resumeSkipFiles = map[string]bool{
	".mapping.json":      true,
	".ds_store":          true,
	"index.json":         true,
	"sw.js":              true,
	"ws-data.json":       true,
	"ws-replay.js":       true,
	"api-responses.har":  true,
	"api-responses.json": true,
	"js-errors.json":     true,
	"sitemap.xml":        true,
	"cookies.json":       true,
	"favicon.ico":        true,
}

var cssURLRe = regexp.MustCompile(`url\(\s*['"]?([^)'"]+)['"]?\s*\)`)
