package storage

import (
	"github.com/user/clone/internal/config"
)

// NewFilesystem creates a Filesystem that writes to the configured output
// directory.
func NewFilesystem(cfg *config.Config) *Filesystem {
	return &Filesystem{
		outputDir: cfg.OutputDir,
		index:     make(map[string]*FileInfo),
	}
}
