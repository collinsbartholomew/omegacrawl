package crawler

import (
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) writeRecord(rec *storage.WARCRecord) {
	if rec == nil {
		return
	}
	if c.cfg.EnableWARC && c.warc != nil {
		if err := c.warc.WriteRecord(rec); err != nil {
			util.LogError("warc write failed", err, zap.String("url", rec.URL))
		}
	}
	if c.cfg.EnableWACZ && c.wacz != nil {
		if err := c.wacz.WriteRecord(rec); err != nil {
			util.LogError("wacz write failed", err, zap.String("url", rec.URL))
		}
	}
}

func (c *Crawler) closeWriters() {
	if c.warc != nil {
		c.warc.Close()
	}
	if c.wacz != nil {
		c.wacz.Close()
	}
}
