package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteIndex serializes the current index to index.json in the output
// directory.
func (fs *Filesystem) WriteIndex() error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if err := os.MkdirAll(fs.outputDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(fs.index, "", "  ")
	if err != nil {
		return err
	}

	indexPath := filepath.Join(fs.outputDir, "index.json")
	return os.WriteFile(indexPath, data, 0644)
}
