package changedetection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"github.com/user/clone/internal/tracing"
	"go.uber.org/zap"
)

// ChangeType represents the type of change detected
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeRemoved  ChangeType = "removed"
	ChangeModified ChangeType = "modified"
)

// Change represents a single detected change
type Change struct {
	Type        ChangeType          `json:"type"`
	Selector    string              `json:"selector"`
	OldValue    string              `json:"old_value,omitempty"`
	NewValue    string              `json:"new_value,omitempty"`
	Attributes  map[string]string   `json:"attributes,omitempty"`
	Timestamp   time.Time           `json:"timestamp"`
}

// Snapshot represents a captured page snapshot for change detection
type Snapshot struct {
	URL         string              `json:"url"`
	Timestamp   time.Time           `json:"timestamp"`
	HTMLHash    string              `json:"html_hash"`
	TextContent string              `json:"text_content,omitempty"`
	Structure   []StructureElement  `json:"structure"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
}

// StructureElement represents an element in the page structure
type StructureElement struct {
	TagName    string            `json:"tag_name"`
	Selector   string            `json:"selector"`
	Attributes map[string]string `json:"attributes"`
	TextHash   string            `json:"text_hash"`
	ChildCount int               `json:"child_count"`
}

// DiffResult contains the results of a diff operation
type DiffResult struct {
	URL         string    `json:"url"`
	OldSnapshot *Snapshot `json:"old_snapshot,omitempty"`
	NewSnapshot *Snapshot `json:"new_snapshot,omitempty"`
	Changes     []Change  `json:"changes"`
	ChangeCount int       `json:"change_count"`
	Timestamp   time.Time `json:"timestamp"`
}

// DetectorConfig configures the change detector
type DetectorConfig struct {
	SnapshotDir    string
	MaxSnapshots   int
	ReportDir      string
	EnableDiff     bool
	IgnoreSelectors []string // CSS selectors to ignore during diff
	MinTextLength  int       // Minimum text length to consider for diff
}

// Detector performs change detection between page snapshots
type Detector struct {
	config     DetectorConfig
	snapshots  map[string][]*Snapshot // URL -> snapshots (newest first)
	mu         sync.RWMutex
	logger     *zap.Logger
}

// NewDetector creates a new change detector
func NewDetector(config DetectorConfig) *Detector {
	if config.SnapshotDir == "" {
		config.SnapshotDir = "./snapshots"
	}
	if config.MaxSnapshots <= 0 {
		config.MaxSnapshots = 10
	}
	if config.MinTextLength <= 0 {
		config.MinTextLength = 10
	}

	// Ensure directories exist
	os.MkdirAll(config.SnapshotDir, 0755)
	if config.ReportDir != "" {
		os.MkdirAll(config.ReportDir, 0755)
	}

	return &Detector{
		config:    config,
		snapshots: make(map[string][]*Snapshot),
		logger:    zap.L().Named("change-detector"),
	}
}

// CaptureSnapshot captures a snapshot of a page for change detection
func (d *Detector) CaptureSnapshot(ctx context.Context, url, html, textContent string, structure []StructureElement, metadata map[string]string) (*Snapshot, error) {
	ctx, span := tracing.StartSpan(ctx, "changedetection.capture",
		tracing.WithAttribute("url", url),
	)
	defer span.End()

	// Compute HTML hash
	hash := sha256.Sum256([]byte(html))
	htmlHash := hex.EncodeToString(hash[:])

	snapshot := &Snapshot{
		URL:         url,
		Timestamp:   time.Now(),
		HTMLHash:    htmlHash,
		TextContent: textContent,
		Structure:   structure,
		Metadata:    metadata,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Load existing snapshots for this URL
	snapshots := d.snapshots[url]
	
	// Check if this is a duplicate (same HTML hash)
	if len(snapshots) > 0 && snapshots[0].HTMLHash == htmlHash {
		d.logger.Debug("Skipping duplicate snapshot",
			zap.String("url", url),
			zap.String("hash", htmlHash[:16]),
		)
		span.SetAttributes(attribute.Bool("duplicate", true))
		return snapshots[0], nil
	}

	// Add new snapshot at the beginning (newest first)
	snapshots = append([]*Snapshot{snapshot}, snapshots...)
	
	// Trim to max snapshots
	if len(snapshots) > d.config.MaxSnapshots {
		snapshots = snapshots[:d.config.MaxSnapshots]
	}
	
	d.snapshots[url] = snapshots

	// Persist to disk
	if err := d.persistSnapshot(url, snapshot); err != nil {
		d.logger.Error("Failed to persist snapshot",
			zap.String("url", url),
			zap.Error(err),
		)
	}

	span.SetAttributes(
		attribute.Int("snapshot_count", len(snapshots)),
		attribute.String("html_hash", htmlHash[:16]),
	)

	return snapshot, nil
}

// CompareWithPrevious compares the latest snapshot with the previous one
func (d *Detector) CompareWithPrevious(ctx context.Context, url string) (*DiffResult, error) {
	ctx, span := tracing.StartSpan(ctx, "changedetection.compare",
		tracing.WithAttribute("url", url),
	)
	defer span.End()

	d.mu.RLock()
	snapshots := d.snapshots[url]
	d.mu.RUnlock()

	if len(snapshots) < 2 {
		return &DiffResult{
			URL:         url,
			NewSnapshot: snapshots[0],
			Changes:     []Change{},
			ChangeCount: 0,
			Timestamp:   time.Now(),
		}, nil
	}

	newSnapshot := snapshots[0]
	oldSnapshot := snapshots[1]

	// Quick check: if HTML hash is same, no changes
	if newSnapshot.HTMLHash == oldSnapshot.HTMLHash {
		return &DiffResult{
			URL:         url,
			OldSnapshot: oldSnapshot,
			NewSnapshot: newSnapshot,
			Changes:     []Change{},
			ChangeCount: 0,
			Timestamp:   time.Now(),
		}, nil
	}

	// Perform detailed diff
	changes := d.diffSnapshots(oldSnapshot, newSnapshot)

	result := &DiffResult{
		URL:         url,
		OldSnapshot: oldSnapshot,
		NewSnapshot: newSnapshot,
		Changes:     changes,
		ChangeCount: len(changes),
		Timestamp:   time.Now(),
	}

	// Generate report if enabled
	if d.config.ReportDir != "" && len(changes) > 0 {
		if err := d.generateReport(result); err != nil {
			d.logger.Error("Failed to generate change report",
				zap.String("url", url),
				zap.Error(err),
			)
		}
	}

	span.SetAttributes(
		attribute.Int("change_count", len(changes)),
	)

	return result, nil
}

// CompareAll compares all URLs that have snapshots
func (d *Detector) CompareAll(ctx context.Context) (map[string]*DiffResult, error) {
	d.mu.RLock()
	urls := make([]string, 0, len(d.snapshots))
	for url := range d.snapshots {
		urls = append(urls, url)
	}
	d.mu.RUnlock()

	results := make(map[string]*DiffResult)
	for _, url := range urls {
		result, err := d.CompareWithPrevious(ctx, url)
		if err != nil {
			d.logger.Error("Failed to compare URL",
				zap.String("url", url),
				zap.Error(err),
			)
			continue
		}
		if result.ChangeCount > 0 {
			results[url] = result
		}
	}

	return results, nil
}

// diffSnapshots performs detailed diff between two snapshots
func (d *Detector) diffSnapshots(old, new *Snapshot) []Change {
	var changes []Change

	// Create maps for easy lookup
	oldStructMap := make(map[string]*StructureElement)
	for i := range old.Structure {
		oldStructMap[old.Structure[i].Selector] = &old.Structure[i]
	}

	newStructMap := make(map[string]*StructureElement)
	for i := range new.Structure {
		newStructMap[new.Structure[i].Selector] = &new.Structure[i]
	}

	// Check for removed elements
	for selector, oldElem := range oldStructMap {
		if _, exists := newStructMap[selector]; !exists {
			if d.shouldIgnoreSelector(selector) {
				continue
			}
			changes = append(changes, Change{
				Type:       ChangeRemoved,
				Selector:   selector,
				OldValue:   oldElem.TextHash,
				Attributes: oldElem.Attributes,
				Timestamp:  new.Timestamp,
			})
		}
	}

	// Check for added and modified elements
	for selector, newElem := range newStructMap {
		oldElem, exists := oldStructMap[selector]
		if !exists {
			if d.shouldIgnoreSelector(selector) {
				continue
			}
			changes = append(changes, Change{
				Type:       ChangeAdded,
				Selector:   selector,
				NewValue:   newElem.TextHash,
				Attributes: newElem.Attributes,
				Timestamp:  new.Timestamp,
			})
		} else if oldElem.TextHash != newElem.TextHash {
			if d.shouldIgnoreSelector(selector) {
				continue
			}
			changes = append(changes, Change{
				Type:        ChangeModified,
				Selector:    selector,
				OldValue:    oldElem.TextHash,
				NewValue:    newElem.TextHash,
				Attributes:  newElem.Attributes,
				Timestamp:   new.Timestamp,
			})
		}
	}

	// Sort changes by selector for consistent output
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Selector < changes[j].Selector
	})

	return changes
}

// shouldIgnoreSelector checks if a selector should be ignored
func (d *Detector) shouldIgnoreSelector(selector string) bool {
	for _, ignore := range d.config.IgnoreSelectors {
		if strings.Contains(selector, ignore) {
			return true
		}
	}
	return false
}

// persistSnapshot saves a snapshot to disk
func (d *Detector) persistSnapshot(url string, snapshot *Snapshot) error {
	// Create a safe filename from URL
	safeURL := strings.ReplaceAll(url, "/", "_")
	safeURL = strings.ReplaceAll(safeURL, ":", "_")
	safeURL = strings.ReplaceAll(safeURL, ".", "_")
	
	filename := fmt.Sprintf("%s_%s.json", safeURL, snapshot.Timestamp.Format("20060102_150405"))
	filepath := filepath.Join(d.config.SnapshotDir, filename)

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// generateReport generates a change report
func (d *Detector) generateReport(result *DiffResult) error {
	filename := fmt.Sprintf("change_report_%s_%s.json",
		strings.ReplaceAll(result.URL, "/", "_"),
		result.Timestamp.Format("20060102_150405"),
	)
	filepath := filepath.Join(d.config.ReportDir, filename)

	report := map[string]interface{}{
		"url":           result.URL,
		"timestamp":     result.Timestamp,
		"change_count":  result.ChangeCount,
		"changes":       result.Changes,
		"old_snapshot":  result.OldSnapshot,
		"new_snapshot":  result.NewSnapshot,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// LoadSnapshots loads snapshots from disk
func (d *Detector) LoadSnapshots() error {
	files, err := os.ReadDir(d.config.SnapshotDir)
	if err != nil {
		return err
	}

	urlSnapshots := make(map[string][]*Snapshot)

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filepath := filepath.Join(d.config.SnapshotDir, file.Name())
		data, err := os.ReadFile(filepath)
		if err != nil {
			d.logger.Warn("Failed to read snapshot file",
				zap.String("file", file.Name()),
				zap.Error(err),
			)
			continue
		}

		var snapshot Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			d.logger.Warn("Failed to parse snapshot file",
				zap.String("file", file.Name()),
				zap.Error(err),
			)
			continue
		}

		urlSnapshots[snapshot.URL] = append(urlSnapshots[snapshot.URL], &snapshot)
	}

	// Sort snapshots by timestamp (newest first) and trim
	d.mu.Lock()
	for url, snapshots := range urlSnapshots {
		sort.Slice(snapshots, func(i, j int) bool {
			return snapshots[i].Timestamp.After(snapshots[j].Timestamp)
		})
		if len(snapshots) > d.config.MaxSnapshots {
			snapshots = snapshots[:d.config.MaxSnapshots]
		}
		d.snapshots[url] = snapshots
	}
	d.mu.Unlock()

	return nil
}

// GetSnapshotCount returns the number of snapshots for a URL
func (d *Detector) GetSnapshotCount(url string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.snapshots[url])
}

// GetAllURLs returns all URLs that have snapshots
func (d *Detector) GetAllURLs() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	
	urls := make([]string, 0, len(d.snapshots))
	for url := range d.snapshots {
		urls = append(urls, url)
	}
	return urls
}

// CleanupOldSnapshots removes snapshots beyond the max limit
func (d *Detector) CleanupOldSnapshots() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for url, snapshots := range d.snapshots {
		if len(snapshots) > d.config.MaxSnapshots {
			// Remove old files
			for _, snapshot := range snapshots[d.config.MaxSnapshots:] {
				safeURL := strings.ReplaceAll(url, "/", "_")
				safeURL = strings.ReplaceAll(safeURL, ":", "_")
				safeURL = strings.ReplaceAll(safeURL, ".", "_")
				
				filename := fmt.Sprintf("%s_%s.json", safeURL, snapshot.Timestamp.Format("20060102_150405"))
				filepath := filepath.Join(d.config.SnapshotDir, filename)
				os.Remove(filepath)
			}
			
			d.snapshots[url] = snapshots[:d.config.MaxSnapshots]
		}
	}

	return nil
}