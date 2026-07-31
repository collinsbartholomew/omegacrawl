package changedetection

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// Snapshot captures the state of a page at a point in time.
type Snapshot struct {
	URL       string            `json:"url"`
	Title     string            `json:"title"`
	Hash      string            `json:"hash"`
	Content   []byte            `json:"content,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Size      int               `json:"size"`
	Checksums map[string]string `json:"checksums,omitempty"`
}

// ChangeType describes the kind of change detected between snapshots.
type ChangeType string

const (
	// ChangeAdded indicates an element was added.
	ChangeAdded ChangeType = "added"
	// ChangeRemoved indicates an element was removed.
	ChangeRemoved ChangeType = "removed"
	// ChangeModified indicates an element changed value.
	ChangeModified ChangeType = "modified"
)

// Change describes a single detected change between snapshots.
type Change struct {
	Type      ChangeType `json:"type"`
	Path      string     `json:"path"`
	OldValue  string     `json:"old_value,omitempty"`
	NewValue  string     `json:"new_value,omitempty"`
	Tag       string     `json:"tag,omitempty"`
	Attribute string     `json:"attribute,omitempty"`
}

// DiffReport summarizes the differences between two snapshots.
type DiffReport struct {
	URL         string    `json:"url"`
	PreviousRun time.Time `json:"previous_run"`
	CurrentRun  time.Time `json:"current_run"`
	Changed     bool      `json:"changed"`
	Changes     []Change  `json:"changes"`
	OldHash     string    `json:"old_hash"`
	NewHash     string    `json:"new_hash"`
}

// Detector stores snapshots and detects changes between page versions.
type Detector struct {
	snapDir   string
	snapshots map[string]*Snapshot
	mu        sync.RWMutex
}

// NewDetector creates a Detector that persists snapshots in snapDir.
func NewDetector(snapDir string) *Detector {
	return &Detector{
		snapDir:   snapDir,
		snapshots: make(map[string]*Snapshot),
	}
}

// SnapshotDir returns the directory where snapshots are stored.
func (d *Detector) SnapshotDir() string {
	return d.snapDir
}

// LoadSnapshot loads the stored snapshot for the given URL, or nil if none exists.
func (d *Detector) LoadSnapshot(url string) (*Snapshot, error) {
	d.mu.RLock()
	if s, ok := d.snapshots[url]; ok {
		d.mu.RUnlock()
		return s, nil
	}
	d.mu.RUnlock()

	snapPath := filepath.Join(d.snapDir, sanitizePath(url)+".json")
	data, err := os.ReadFile(snapPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.snapshots[url] = &snap
	d.mu.Unlock()
	return &snap, nil
}

// SaveSnapshot stores a snapshot for the given URL and content.
func (d *Detector) SaveSnapshot(url, title string, content []byte) (*Snapshot, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	snap := &Snapshot{
		URL:       url,
		Title:     title,
		Hash:      hash,
		Content:   content,
		Timestamp: time.Now(),
		Size:      len(content),
	}

	if err := os.MkdirAll(d.snapDir, 0755); err != nil {
		return snap, err
	}
	snapPath := filepath.Join(d.snapDir, sanitizePath(url)+".json")
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return snap, err
	}
	if err := os.WriteFile(snapPath, data, 0600); err != nil {
		return snap, err
	}

	d.mu.Lock()
	d.snapshots[url] = snap
	d.mu.Unlock()
	return snap, nil
}

// DetectChanges compares two snapshots and returns a report of any differences.
func (d *Detector) DetectChanges(url string, old, new *Snapshot) *DiffReport {
	report := &DiffReport{
		URL:         url,
		PreviousRun: old.Timestamp,
		CurrentRun:  new.Timestamp,
		OldHash:     old.Hash,
		NewHash:     new.Hash,
	}
	if old.Hash == new.Hash {
		return report
	}
	report.Changed = true
	report.Changes = computeDiff(old.Content, new.Content)
	return report
}

func computeDiff(oldContent, newContent []byte) []Change {
	oldDoc, oldErr := html.Parse(bytes.NewReader(oldContent))
	newDoc, newErr := html.Parse(bytes.NewReader(newContent))
	if oldErr == nil && newErr == nil {
		changes := diffNodes(oldDoc, newDoc, "/")
		if len(changes) > 100 {
			changes = changes[:100]
		}
		return changes
	}

	oldLines := bytes.Split(oldContent, []byte("\n"))
	newLines := bytes.Split(newContent, []byte("\n"))
	changes := make([]Change, 0)
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	for i := 0; i < maxLen; i++ {
		if i >= len(oldLines) {
			changes = append(changes, Change{
				Type:     ChangeAdded,
				Path:     fmt.Sprintf("line:%d", i+1),
				NewValue: string(newLines[i]),
			})
		} else if i >= len(newLines) {
			changes = append(changes, Change{
				Type:     ChangeRemoved,
				Path:     fmt.Sprintf("line:%d", i+1),
				OldValue: string(oldLines[i]),
			})
		} else if !bytes.Equal(oldLines[i], newLines[i]) {
			changes = append(changes, Change{
				Type:     ChangeModified,
				Path:     fmt.Sprintf("line:%d", i+1),
				OldValue: string(oldLines[i]),
				NewValue: string(newLines[i]),
			})
		}
	}
	if len(changes) > 100 {
		changes = changes[:100]
	}
	return changes
}

func diffNodes(old, new *html.Node, path string) []Change {
	var changes []Change
	if old.Type != new.Type {
		changes = append(changes, Change{
			Type:     ChangeModified,
			Path:     path,
			OldValue: old.Data,
			NewValue: new.Data,
		})
		return changes
	}
	if old.Type == html.TextNode || old.Type == html.CommentNode {
		if !bytes.Equal([]byte(old.Data), []byte(new.Data)) {
			changes = append(changes, Change{
				Type:     ChangeModified,
				Path:     path,
				OldValue: old.Data,
				NewValue: new.Data,
			})
		}
		return changes
	}
	if old.Data != new.Data {
		changes = append(changes, Change{
			Type:     ChangeModified,
			Path:     path,
			OldValue: old.Data,
			NewValue: new.Data,
			Tag:      old.Data,
		})
	}
	oldAttrs := make(map[string]string)
	for _, a := range old.Attr {
		oldAttrs[a.Key] = a.Val
	}
	newAttrs := make(map[string]string)
	for _, a := range new.Attr {
		newAttrs[a.Key] = a.Val
	}
	for k, ov := range oldAttrs {
		if nv, ok := newAttrs[k]; ok {
			if ov != nv {
				changes = append(changes, Change{
					Type:      ChangeModified,
					Path:      path + "@" + k,
					OldValue:  ov,
					NewValue:  nv,
					Attribute: k,
				})
			}
		} else {
			changes = append(changes, Change{
				Type:     ChangeRemoved,
				Path:     path + "@" + k,
				OldValue: ov,
			})
		}
	}
	for k, nv := range newAttrs {
		if _, ok := oldAttrs[k]; !ok {
			changes = append(changes, Change{
				Type:     ChangeAdded,
				Path:     path + "@" + k,
				NewValue: nv,
			})
		}
	}

	oldChild := old.FirstChild
	newChild := new.FirstChild
	idx := 0
	for oldChild != nil && newChild != nil {
		childPath := fmt.Sprintf("%s/%s[%d]", path, oldChild.Data, idx)
		changes = append(changes, diffNodes(oldChild, newChild, childPath)...)
		oldChild = oldChild.NextSibling
		newChild = newChild.NextSibling
		idx++
	}
	for oldChild != nil {
		changes = append(changes, Change{
			Type: ChangeRemoved,
			Path: fmt.Sprintf("%s/%s[%d]", path, oldChild.Data, idx),
		})
		oldChild = oldChild.NextSibling
		idx++
	}
	for newChild != nil {
		changes = append(changes, Change{
			Type:     ChangeAdded,
			Path:     fmt.Sprintf("%s/%s[%d]", path, newChild.Data, idx),
			NewValue: newChild.Data,
		})
		newChild = newChild.NextSibling
		idx++
	}
	if len(changes) > 100 {
		changes = changes[:100]
	}
	return changes
}

func sanitizePath(url string) string {
	return base64.URLEncoding.EncodeToString([]byte(url))
}
