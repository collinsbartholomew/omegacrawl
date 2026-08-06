package crawler

import (
	"encoding/gob"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/storage"
)

// SeedResumeFromDisk rebuilds a crawl checkpoint from an interrupted run's
// output directory so the crawler can continue instead of starting over. Files
// already saved on disk are marked visited, and absolute http(s) URLs still
// referenced by those pages but not yet saved become the pending queue.
func SeedResumeFromDisk(outputDir, checkpointFile string) (pending, visited int, err error) {
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return 0, 0, err
	}

	visitedSet := make(map[string]bool)
	pendingSet := make(map[string]bool)
	var pendingItems []queue.URLItem
	fstore := storage.NewFilesystem(&config.Config{OutputDir: absOut})

	alreadyOnDisk := func(rawURL string) bool {
		p := fstore.PathForURL(rawURL)
		if p == "" {
			return false
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
		return false
	}

	walkErr := filepath.WalkDir(absOut, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if resumeSkipFiles[base] {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(base))
		if ext == ".png" || ext == ".pdf" || ext == ".har" {
			return nil
		}
		rel, rerr := filepath.Rel(absOut, p)
		if rerr != nil {
			return nil
		}
		u := urlFromPath(rel)
		if u == "" {
			return nil
		}
		norm := queue.NormalizeAndClean(u)
		if norm != "" && !visitedSet[norm] {
			visitedSet[norm] = true
			markDirectoryAliases(visitedSet, norm)
		}

		if ext == ".html" || ext == ".htm" || ext == ".css" {
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			for _, ref := range extractURLRefs(string(data), u) {
				rnorm := queue.NormalizeAndClean(ref)
				if rnorm == "" || visitedSet[rnorm] || pendingSet[rnorm] || len(pendingItems) >= 200000 {
					continue
				}
				if isResumeNoise(rnorm) {
					continue
				}

				if alreadyOnDisk(ref) {
					visitedSet[rnorm] = true
					continue
				}
				pendingSet[rnorm] = true
				pendingItems = append(pendingItems, queue.URLItem{URL: rnorm, Depth: 1})
			}
		}
		return nil
	})
	if walkErr != nil {
		return 0, 0, walkErr
	}

	state := &CheckpointData{
		Queue:         pendingItems,
		Visited:       visitedSet,
		HostLastCrawl: map[string]time.Time{},
		HostURLCount:  map[string]int{},
		Timestamp:     time.Now(),
	}
	if err := os.MkdirAll(filepath.Dir(checkpointFile), 0755); err != nil {
		return 0, 0, err
	}
	tmp := checkpointFile + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, err
	}
	if err := gob.NewEncoder(f).Encode(state); err != nil {
		f.Close()
		os.Remove(tmp)
		return 0, 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return 0, 0, err
	}
	return len(pendingItems), len(visitedSet), os.Rename(tmp, checkpointFile)
}

// markDirectoryAliases also registers the bare directory forms of an
// index.html page so references like /dir/ and /dir are recognized as visited.
func markDirectoryAliases(visited map[string]bool, normalized string) {
	if !strings.HasSuffix(normalized, "/index.html") {
		return
	}
	base := strings.TrimSuffix(normalized, "index.html")
	visited[base] = true
	visited[strings.TrimSuffix(base, "/")] = true
}

// isResumeNoise reports whether a pending URL is a low-value variant (session
// actions, pagination, feeds, admin/api endpoints) that would only re-render
// shared assets and burn bandwidth without adding useful offline content.
func isResumeNoise(raw string) bool {
	if strings.Contains(raw, "?p=") || strings.Contains(raw, "_wpnonce") ||
		strings.Contains(raw, "remove_item") || strings.Contains(raw, "add-to-cart") ||
		strings.Contains(raw, "replytocom") || strings.Contains(raw, "&cpage=") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	low := strings.ToLower(u.Path)
	for _, frag := range []string{"/page/", "/author/", "/cart", "/checkout", "/my-account",
		"/oembed", "/feed", "/xmlrpc", "/wp-json", "/wp-admin", "/comments", "/respond",
		"/tag/", "/trackback/"} {
		if strings.Contains(low, frag) {
			return true
		}
	}
	return false
}
