package crawler

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

// writeMappingFile persists the full URL -> local-path mapping so the
// localization pass can rebuild a complete rewrite mapping, including the
// content-hash dedup aliases that a reverse scan of the files cannot recover.
func (c *Crawler) writeMappingFile() {
	mappings := c.rewriter.GetMappings()
	if len(mappings) == 0 {
		return
	}
	data, err := json.Marshal(mappings)
	if err != nil {
		util.LogError("failed to marshal mapping", err)
		return
	}
	path := filepath.Join(c.cfg.OutputDir, ".mapping.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write mapping file", err, zap.String("path", path))
		return
	}
	util.LogInfo("wrote mapping file", zap.String("path", path), zap.Int("entries", len(mappings)))
}
