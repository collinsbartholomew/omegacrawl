package localize

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestTree(root, rel string) error {
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("<html><head></head><body>x</body></html>"), 0644)
}

func TestCanonicalKeyDefault(t *testing.T) {
	c := newCanonicalizer(nil, nil)
	cases := map[string]string{
		"https://example.com/blog":                              "https://example.com/blog",
		"https://example.com/blog/page/2":                       "https://example.com/blog",
		"https://example.com/blog/paged/3":                      "https://example.com/blog",
		"https://example.com/blog/comment-page-2":               "https://example.com/blog",
		"https://example.com/news/2026/05/12/slug":              "https://example.com/news/2026/05/12/slug",
		"https://example.com/blog?page=2":                       "https://example.com/blog",
		"https://example.com/blog?utm_source=x&utm_medium=y":    "https://example.com/blog",
		"https://example.com/blog?orderby=date&page=2":          "https://example.com/blog?orderby=date",
		"https://example.com/blog?orderby=price":                "https://example.com/blog?orderby=price",
		"https://example.com/shop?gclid=abc&orderby=popularity": "https://example.com/shop?orderby=popularity",
	}
	for in, want := range cases {
		if got := c.canonicalKey(in); got != want {
			t.Errorf("canonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalKeyPreserveQueryParam(t *testing.T) {
	c := newCanonicalizer([]string{"paged"}, nil)
	if got := c.canonicalKey("https://example.com/blog?paged=4"); got != "https://example.com/blog?paged=4" {
		t.Errorf("preserved paged should stay: got %q", got)
	}
	if got := c.canonicalKey("https://example.com/blog?paged=4&utm_source=x"); got != "https://example.com/blog?paged=4" {
		t.Errorf("utm should still drop while paged stays: got %q", got)
	}
}

func TestCanonicalKeyPreservePathSegment(t *testing.T) {
	c := newCanonicalizer(nil, []string{"page"})
	if got := c.canonicalKey("https://example.com/blog/page/2"); got != "https://example.com/blog/page/2" {
		t.Errorf("preserved page segment should stay: got %q", got)
	}
	if got := c.canonicalKey("https://example.com/blog/paged/3"); got != "https://example.com/blog" {
		t.Errorf("unpreserved paged should still drop: got %q", got)
	}
}

func TestIsNoisePath(t *testing.T) {
	if !isNoisePath("https://example.com/wp-json/wp/v2/posts") {
		t.Error("wp-json should be noise")
	}
	if !isNoisePath("https://example.com/cart") {
		t.Error("cart should be noise")
	}
	if isNoisePath("https://example.com/blog") {
		t.Error("blog should not be noise")
	}
}

func TestDedupPreservesPathSegment(t *testing.T) {
	root := t.TempDir()
	dir := root + "/site"
	mk := func(rel string) {
		if err := writeTestTree(dir, rel); err != nil {
			t.Fatal(err)
		}
	}
	mk("example.com/blog/index.html")
	mk("example.com/blog/page/2/index.html")
	mk("example.com/blog/paged/3/index.html")

	dst := root + "/out"
	rep, err := Dedup(dir, dst, DedupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.PagesKept != 1 {
		t.Errorf("default: expected 1 kept, got %d (collapsed %d)", rep.PagesKept, rep.PagesCollapsed)
	}

	dst2 := root + "/out2"
	rep2, err := Dedup(dir, dst2, DedupOptions{PreservePathSegments: []string{"page"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.PagesKept != 2 {
		t.Errorf("preserving page: expected 2 kept, got %d (collapsed %d)", rep2.PagesKept, rep2.PagesCollapsed)
	}
}