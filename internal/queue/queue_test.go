package queue

import (
	"testing"
)

func TestNormalizeURL_TrackingParams(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip utm params",
			input:    "https://example.com/page?utm_source=google&utm_medium=campaign&id=123",
			expected: "https://example.com/page?id=123",
		},
		{
			name:     "strip fbclid",
			input:    "https://example.com/page?fbclid=abc123&data=test",
			expected: "https://example.com/page?data=test",
		},
		{
			name:     "strip all tracking params",
			input:    "https://example.com/page?utm_source=a&utm_medium=b&utm_campaign=c&utm_term=d&utm_content=e",
			expected: "https://example.com/page",
		},
		{
			name:     "sort query params",
			input:    "https://example.com/page?z=1&a=2&m=3",
			expected: "https://example.com/page?a=2&m=3&z=1",
		},
		{
			name:     "strip fragment",
			input:    "https://example.com/page#section",
			expected: "https://example.com/page",
		},
		{
			name:     "normalize protocol",
			input:    "HTTP://EXAMPLE.COM/Path",
			expected: "http://example.com/Path",
		},
		{
			name:     "strip default port",
			input:    "https://example.com:443/page",
			expected: "https://example.com/page",
		},
		{
			name:     "strip trailing slash",
			input:    "https://example.com/page/",
			expected: "https://example.com/page",
		},
		{
			name:     "keep root slash",
			input:    "https://example.com/",
			expected: "https://example.com/",
		},
		{
			name:     "normalize percent encoding",
			input:    "https://example.com/path%7Eto%7Efile",
			expected: "https://example.com/path~to~file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeURL(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAndClean(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip fragment and tracking",
			input:    "https://example.com/page?utm_source=test#section",
			expected: "https://example.com/page",
		},
		{
			name:     "clean path",
			input:    "https://example.com/a/../b/./c",
			expected: "https://example.com/b/c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeAndClean(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeAndClean(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBloomDedup(t *testing.T) {
	b := NewBloomDedup(1000, 0.01)

	urls := []string{
		"https://example.com/page1",
		"https://example.com/page2",
		"https://example.com/page3",
	}

	for _, u := range urls {
		b.Add(u)
	}

	for _, u := range urls {
		if !b.HasSeen(u) {
			t.Errorf("expected HasSeen(%s) = true", u)
		}
	}

	if b.HasSeen("https://example.com/notseen") {
		t.Error("expected HasSeen for unseen URL to be false (or rare false positive)")
	}

	count := b.Count()
	if count < 3 {
		t.Errorf("expected count >= 3, got %d", count)
	}
}

func TestBloomDedup_Persistence(t *testing.T) {
	tmpFile := t.TempDir() + "/bloom.gob"

	b1 := NewBloomDedup(1000, 0.01)
	b1.Add("https://example.com/test")
	b1.SaveToFile(tmpFile)

	b2 := NewBloomDedup(1000, 0.01)
	err := b2.LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to load bloom filter: %v", err)
	}

	if !b2.HasSeen("https://example.com/test") {
		t.Error("expected loaded bloom filter to remember URL")
	}
}

func TestPriorityQueue_ItemsAndAllVisited(t *testing.T) {
	q := NewPriorityQueue()

	q.PushURL("https://a.com", 0)
	q.PushURL("https://b.com", 1)
	q.PushURL("https://c.com", 2)

	items := q.Items()
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}

	visited := q.AllVisited()
	if len(visited) != 3 {
		t.Errorf("expected 3 visited, got %d", len(visited))
	}

	if !visited["https://a.com"] {
		t.Error("expected https://a.com to be visited")
	}
}

func TestPriorityQueue_LoadFromCheckpoint(t *testing.T) {
	q := NewPriorityQueue()

	items := []URLItem{
		{URL: "https://a.com", Depth: 0},
		{URL: "https://b.com", Depth: 1},
	}
	visited := map[string]bool{
		"https://a.com": true,
		"https://b.com": true,
	}

	q.LoadFromCheckpoint(items, visited)

	if q.Size() != 2 {
		t.Errorf("expected 2 items, got %d", q.Size())
	}

	if !q.HasSeen("https://a.com") {
		t.Error("expected https://a.com to be seen")
	}
}

func TestPriorityQueue_Dedup(t *testing.T) {
	q := NewPriorityQueue()

	if !q.PushURL("https://example.com", 0) {
		t.Error("expected first push to succeed")
	}

	if q.PushURL("https://example.com", 0) {
		t.Error("expected duplicate push to fail")
	}

	if q.Size() != 1 {
		t.Errorf("expected length 1, got %d", q.Size())
	}
}
