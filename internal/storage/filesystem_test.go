package storage

import (
	"os"
	"testing"

	"github.com/user/clone/internal/config"
)

func TestFilesystem_SaveFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{OutputDir: dir}
	fs := NewFilesystem(cfg)

	data := []byte("test content")
	path, err := fs.SaveFile("https://example.com/assets/test.txt", data, "text/plain")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	if string(saved) != string(data) {
		t.Fatalf("saved content mismatch: got %s, want %s", saved, data)
	}

	expectedSuffix := "example.com/assets/test.txt"
	if path[len(path)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("path should mirror URL structure, got: %s", path)
	}
}

func TestFilesystem_SaveHTML(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{OutputDir: dir}
	fs := NewFilesystem(cfg)

	data := []byte("<html><body>Hello</body></html>")
	path, err := fs.SaveHTML("https://example.com/page/index.html", data)
	if err != nil {
		t.Fatalf("SaveHTML failed: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved HTML: %v", err)
	}

	if string(saved) != string(data) {
		t.Fatalf("saved content mismatch: got %s, want %s", saved, data)
	}

	expectedSuffix := "example.com/page/index.html"
	if path[len(path)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("path should mirror URL structure, got: %s", path)
	}
}

func TestFilesystem_QueryStringInPath(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{OutputDir: dir}
	fs := NewFilesystem(cfg)

	data := []byte("response")
	path, err := fs.SaveFile("https://api.example.com/data?id=123&format=json", data, "application/json")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist at path: %s", path)
	}
}

func TestFilesystem_WriteIndex(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{OutputDir: dir}
	fs := NewFilesystem(cfg)

	fs.SaveFile("https://example.com/test.txt", []byte("test"), "text/plain")

	err := fs.WriteIndex()
	if err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	_, err = os.Stat(dir + "/index.json")
	if err != nil {
		t.Fatalf("index.json not created: %v", err)
	}
}

func TestFilesystem_MirrorStructure(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{OutputDir: dir}
	fs := NewFilesystem(cfg)

	urls := []struct {
		url string
		ext string
	}{
		{"https://example.com/index.html", ".html"},
		{"https://example.com/about/index.html", ".html"},
		{"https://example.com/assets/style.css", ".css"},
		{"https://example.com/assets/script.js", ".js"},
		{"https://example.com/images/logo.png", ".png"},
	}

	for _, u := range urls {
		path := fs.PathForURL(u.url)
		expectedPrefix := dir + "/example.com/"
		if len(path) < len(expectedPrefix) || path[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("PathForURL(%s) = %s, should start with %s", u.url, path, expectedPrefix)
		}
	}
}
