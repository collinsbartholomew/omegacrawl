package repair

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
)

// Options configures the repair pass.
type Options struct {
	OutputDir  string
	UserAgent  string
	MaxWorkers int
}

// Report summarizes what the repair pass did.
type Report struct {
	PagesScanned     int
	AssetsReferenced int
	AssetsMissing    int
	AssetsDownloaded int
	AssetsFailed     int
	AssetsRewritten  int
	FailureURLs      []string
}

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Run scans the output directory, downloads missing assets referenced by the
// saved pages, and re-rewrites every page with the complete URL mapping.
func Run(opts Options) (*Report, error) {
	rep := &Report{}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = 5
	}

	root, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return rep, err
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return rep, errors.New("output directory not found: " + opts.OutputDir)
	}

	fs := storage.NewFilesystem(&config.Config{OutputDir: root})

	pages, err := findPages(root)
	if err != nil {
		return rep, err
	}
	rep.PagesScanned = len(pages)

	mapping := make(map[string]string)
	missing := make(map[string]bool)
	pageInfo := make([]pageRef, 0, len(pages))

	classify := func(u string) {
		rep.AssetsReferenced++
		if fileExists(fs, u) {
			mapping[u] = fs.PathForURL(u)
			return
		}
		missing[u] = true
	}

	for _, p := range pages {
		pageURL := urlFromLocalPath(root, p)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		pageInfo = append(pageInfo, pageRef{path: p, url: pageURL})
		for u := range extractAssetURLs(data, pageURL) {
			classify(u)
		}
	}

	stylesheets, err := findStylesheets(root)
	if err != nil {
		return rep, err
	}
	for _, c := range stylesheets {
		pageURL := urlFromLocalPath(root, c)
		data, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		pageInfo = append(pageInfo, pageRef{path: c, url: pageURL})
		for _, cssURL := range extractCSSURLs(string(data)) {

			if !strings.HasPrefix(cssURL, "http://") && !strings.HasPrefix(cssURL, "https://") {
				continue
			}
			classify(cssURL)
		}
	}
	rep.AssetsMissing = len(missing)

	if len(missing) > 0 {
		if err := downloadMissing(fs, missing, opts.UserAgent, opts.MaxWorkers, rep); err != nil {
			return rep, err
		}
	}

	for u := range missing {
		p := fs.PathForURL(u)
		if fileExists(fs, u) {
			mapping[u] = p
		}
	}
	rep.AssetsRewritten = len(mapping)

	if len(mapping) == 0 {
		return rep, nil
	}

	for _, pr := range pageInfo {
		data, err := os.ReadFile(pr.path)
		if err != nil {
			continue
		}
		rewritten := rewriteRepairedURLs(data, pr.path, mapping)
		if err := os.WriteFile(pr.path, rewritten, 0644); err != nil {
			util.LogError("repair: failed to write rewritten page", err, zap.String("path", pr.path))
		}
	}

	return rep, nil
}
