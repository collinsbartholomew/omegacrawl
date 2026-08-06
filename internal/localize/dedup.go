package localize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DedupReport summarizes a deduplicated export.
type DedupReport struct {
	Src            string              `json:"src"`
	Dst            string              `json:"dst"`
	FilesTotal     int                 `json:"files_total"`
	PagesTotal     int                 `json:"pages_total"`
	PagesKept      int                 `json:"pages_kept"`
	PagesCollapsed int                 `json:"pages_collapsed"`
	AssetsCopied   int                 `json:"assets_copied"`
	Manifest       map[string][]string `json:"manifest,omitempty"`
}

// DedupOptions configures the deduplication pass.
type DedupOptions struct {
	// PreserveQueryParams lists query keys that select distinct content and
	// must not be treated as duplication noise (overrides the defaults).
	PreserveQueryParams []string
	// PreservePathSegments lists path segments that are real content rather
	// than pagination markers (overrides the defaults).
	PreservePathSegments []string
}

// Dedup produces a slimmed copy of a clone tree: assets and unique pages are
// copied wholesale, while duplicate/permutation page variants are collapsed to
// a single representative per unique document. Representatives stay at their
// original relative location so already-localized references remain valid.
func Dedup(srcRoot, dst string, opts DedupOptions) (*DedupReport, error) {
	rep := &DedupReport{Src: srcRoot, Dst: dst, Manifest: map[string][]string{}}
	if err := os.RemoveAll(dst); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return nil, err
	}

	var files []string
	err := filepath.Walk(srcRoot, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		files = append(files, p)
		rep.FilesTotal++
		return nil
	})
	if err != nil {
		return nil, err
	}

	canon := newCanonicalizer(opts.PreserveQueryParams, opts.PreservePathSegments)

	type group struct {
		repFile   string
		repWeight int
		collapsed []string
	}
	groups := map[string]*group{}
	for _, p := range files {
		if !isHTMLPagePath(p) {
			continue
		}
		rep.PagesTotal++
		u := urlFromLocalPath(srcRoot, p)
		if isNoisePath(u) {
			rep.PagesCollapsed++
			continue
		}
		key := canon.canonicalKey(u)
		w := cleanWeight(u)
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{repFile: p, repWeight: w}
			rep.PagesKept++
			continue
		}
		g.collapsed = append(g.collapsed, relWithin(srcRoot, p))
		rep.PagesCollapsed++
		// Prefer the lighter (less noisy) representative; on a tie choose the
		// lexicographically-first path so selection is deterministic regardless
		// of filesystem iteration order.
		if w < g.repWeight || (w == g.repWeight && relWithin(srcRoot, p) < relWithin(srcRoot, g.repFile)) {
			g.repFile, g.repWeight = p, w
		}
	}

	kept := map[string]bool{}
	for _, g := range groups {
		kept[g.repFile] = true
		if len(g.collapsed) > 0 {
			sortStrings(g.collapsed)
			rep.Manifest[relWithin(srcRoot, g.repFile)] = g.collapsed
		}
	}

	for _, p := range files {
		if isHTMLPagePath(p) && !kept[p] {
			continue
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		dest := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return nil, err
		}
		if err := copyFile(p, dest); err != nil {
			return nil, err
		}
		if isHTMLPagePath(p) {
			continue
		}
		rep.AssetsCopied++
	}

	buf, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(filepath.Join(dst, "dedupe-manifest.json"), buf, 0644)
	return rep, nil
}
