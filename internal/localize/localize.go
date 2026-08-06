package localize

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

// Options configures the localization pass.
type Options struct {
	CloneDir     string // directory holding the raw downloads
	LocalizedDir string // directory to write the rewritten copy into
	MaxWorkers   int
}

// Report summarizes what the localization pass did.
type Report struct {
	FilesCopied    int
	PagesScanned   int
	PagesRewritten int
	CSSFiles       int
	CSSRewritten   int
	JSFiles        int
	JSRewritten    int
	Mappings       int
}

const mappingFileName = ".mapping.json"

// Run copies the raw clone into the localized directory and rewrites every
// page and stylesheet so all references resolve to local files. It is safe to
// re-run; the localized directory is rebuilt from scratch each time.
func Run(opts Options) (*Report, error) {
	rep := &Report{}
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = 5
	}

	cloneRoot, err := filepath.Abs(opts.CloneDir)
	if err != nil {
		return rep, err
	}
	if fi, err := os.Stat(cloneRoot); err != nil || !fi.IsDir() {
		return rep, os.ErrNotExist
	}
	localRoot, err := filepath.Abs(opts.LocalizedDir)
	if err != nil {
		return rep, err
	}

	if localRoot == cloneRoot || strings.HasPrefix(cloneRoot+string(filepath.Separator), localRoot+string(filepath.Separator)) {
		return rep, fmt.Errorf("localized directory must not be the clone directory or an ancestor of it (clone=%s localized=%s)", cloneRoot, localRoot)
	}

	// 1. Fresh copy of the raw clone into the localized directory.
	if err := os.RemoveAll(localRoot); err != nil {
		return rep, err
	}
	if err := os.MkdirAll(localRoot, 0755); err != nil {
		return rep, err
	}
	rep.FilesCopied, err = copyTree(cloneRoot, localRoot)
	if err != nil {
		return rep, err
	}

	// 2. Build the complete URL -> localized-path mapping.
	urlToLocal, pageURLs, err := buildMapping(cloneRoot, localRoot)
	if err != nil {
		return rep, err
	}
	rep.Mappings = len(urlToLocal)

	// 3. Rewrite every page and stylesheet in the localized copy.
	rw := NewRewriter(urlToLocal, pageURLs, localRoot)
	err = rewriteTree(rw, localRoot, rep)
	if err != nil {
		return rep, err
	}

	// 4. Rewrite JavaScript bundles (webpack/Next.js/React/Vue)
	jsrw := NewJSReWriter(urlToLocal, pageURLs, localRoot)
	err = rewriteJS(jsrw, localRoot, rep)
	if err != nil {
		return rep, err
	}

	util.LogInfo("localize complete",
		zap.Int("files_copied", rep.FilesCopied),
		zap.Int("pages_scanned", rep.PagesScanned),
		zap.Int("pages_rewritten", rep.PagesRewritten),
		zap.Int("css_rewritten", rep.CSSRewritten),
		zap.Int("js_files", rep.JSFiles),
		zap.Int("js_rewritten", rep.JSRewritten),
		zap.Int("mappings", rep.Mappings),
	)
	return rep, nil
}

// copyTree copies every file from src into dst (excluding the mapping file).
// If dst lives inside src (legacy single-directory clones), the dst subtree is
// skipped so the walk never re-copies the output into itself.
func copyTree(src, dst string) (int, error) {
	count := 0
	srcClean := filepath.Clean(src)
	dstClean := filepath.Clean(dst)
	dstPrefix := dstClean + string(filepath.Separator)

	// Check if destination is inside source (legacy single-directory case)
	// If so, we need to skip the destination subtree to avoid re-copying
	dstInsideSrc := strings.HasPrefix(dstClean, srcClean+string(filepath.Separator))

	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		clean := filepath.Clean(p)

		// Skip destination subtree if it lives inside source (legacy case)
		if dstInsideSrc && (clean == dstClean || strings.HasPrefix(clean, dstPrefix)) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if fi.Name() == mappingFileName || fi.Name() == ".ds_store" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return nil
		}
		count++
		return nil
	})
	return count, err
}

// buildMapping returns urlToLocal (absolute URL -> localized path) and
// pageURLs (localized file path -> source URL). The persisted crawler mapping
// is authoritative (it includes content-hash dedup aliases); a reverse scan
// of the clone fills in any URLs the mapping did not record.
func buildMapping(cloneRoot, localRoot string) (map[string]string, map[string]string, error) {
	urlToLocal := make(map[string]string)
	pageURLs := make(map[string]string)

	addURL := func(u, lp string) {
		if u == "" || lp == "" {
			return
		}
		// Record both the %20-encoded and raw-space forms so references match
		// regardless of how they appear in the saved HTML.
		urlToLocal[u] = lp
		if d, err := url.PathUnescape(u); err == nil && d != u {
			urlToLocal[d] = lp
		}
		// Directory pages are referenced as /dir, /dir/ or /dir/index.html but
		// stored as dir/index.html. Register all three URL forms.
		if strings.HasSuffix(u, "/index.html") {
			base := strings.TrimSuffix(u, "/index.html")
			if base != "" {
				urlToLocal[base+"/"] = lp
				urlToLocal[base] = lp
			}
		}
	}

	loadFile := func(mf string, toLocal func(string) string) error {
		data, err := os.ReadFile(mf)
		if err != nil {
			return err
		}

		// Try to parse as map[string]string (legacy mapping file)
		var m map[string]string
		if err := json.Unmarshal(data, &m); err == nil {
			for u, p := range m {
				lp := toLocal(p)
				if lp == "" {
					continue
				}
				addURL(u, lp)
				if isHTMLPagePath(lp) || isCSSPath(lp) {
					pageURLs[lp] = u
				}
			}
			return nil
		}

		// Try to parse as map[string]StorageEntry (index.json format)
		var m2 map[string]struct {
			URL       string `json:"url"`
			LocalPath string `json:"local_path"`
		}
		if err := json.Unmarshal(data, &m2); err == nil {
			for u, entry := range m2 {
				lp := toLocal(entry.LocalPath)
				if lp == "" {
					continue
				}
				addURL(u, lp)
				if isHTMLPagePath(lp) || isCSSPath(lp) {
					pageURLs[lp] = u
				}
			}
			return nil
		}

		return fmt.Errorf("unknown format for %s", mf)
	}

	// Persisted crawler mapping (url -> path inside the clone).
	_ = loadFile(filepath.Join(cloneRoot, mappingFileName), func(p string) string {
		return remapPath(cloneRoot, localRoot, p)
	})
	// Storage index.json also records url -> path.
	if err := loadFile(filepath.Join(cloneRoot, "index.json"), func(p string) string {
		return remapPath(cloneRoot, localRoot, p)
	}); err != nil {
		util.LogError("failed to load storage index.json", err)
	}

	// Reverse scan: cover any file the mappings missed (e.g. legacy clones).
	_ = filepath.Walk(cloneRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		lp, ok := remapPathOK(cloneRoot, localRoot, p)
		if !ok {
			return nil
		}
		u := urlFromLocalPath(cloneRoot, p)
		if u == "" {
			return nil
		}
		if isHTMLPagePath(p) || isCSSPath(p) {
			// Only set pageURL if not already set from persisted mapping (which is authoritative).
			if _, exists := pageURLs[lp]; !exists {
				pageURLs[lp] = u
			}
		}
		if _, exists := urlToLocal[u]; !exists {
			addURL(u, lp)
		}
		return nil
	})

	return urlToLocal, pageURLs, nil
}

// remapPath converts a clone-side path to the corresponding localized path.
func remapPath(cloneRoot, localRoot, p string) string {
	lp, _ := remapPathOK(cloneRoot, localRoot, p)
	return lp
}

func remapPathOK(cloneRoot, localRoot, p string) (string, bool) {
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(cloneRoot, absP)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.Join(localRoot, rel), true
}

// rewriteTree walks the localized directory and rewrites every HTML page and
// stylesheet in place.
func rewriteTree(rw *Rewriter, localRoot string, rep *Report) error {
	return filepath.Walk(localRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if isCSSPath(p) {
			rep.CSSFiles++
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			out := rw.RewriteCSS(data, p)
			if !strings.EqualFold(string(out), string(data)) {
				if werr := os.WriteFile(p, out, 0644); werr == nil {
					rep.CSSRewritten++
				}
			}
			return nil
		}
		if isHTMLPagePath(p) {
			if isBinaryHTML(p) {
				return nil
			}
			rep.PagesScanned++
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			out := rw.RewriteHTML(data, p)
			if !strings.EqualFold(string(out), string(data)) {
				if werr := os.WriteFile(p, out, 0644); werr == nil {
					rep.PagesRewritten++
				}
			}
			return nil
		}
		return nil
	})
}

// isJSPath reports whether the file path is a JavaScript file.
func isJSPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".js" || ext == ".mjs" || ext == ".cjs"
}

// rewriteJS walks the localized directory and rewrites JavaScript bundles.
func rewriteJS(rw *JSReWriter, localRoot string, rep *Report) error {
	return filepath.Walk(localRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if !isJSPath(p) {
			return nil
		}
		rep.JSFiles++
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		out, changed := rw.RewriteJS(data, p)
		if changed {
			if werr := os.WriteFile(p, out, 0644); werr == nil {
				rep.JSRewritten++
			}
		}
		return nil
	})
}
