package crawler

import (
	"encoding/json"
	"os"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func (c *Crawler) writeJSErrors() {
	errs := c.jsErrors.GetAll()
	if len(errs) == 0 {
		return
	}
	jsErrors := make([]JSError, 0, len(errs))
	for _, e := range errs {
		if je, ok := e.(JSError); ok {
			jsErrors = append(jsErrors, je)
		}
	}
	if len(jsErrors) == 0 {
		return
	}
	data, err := json.MarshalIndent(jsErrors, "", "  ")
	if err != nil {
		util.LogError("failed to marshal JS errors", err)
		return
	}
	path := c.cfg.OutputDir + "/js-errors.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write JS errors", err)
		return
	}
	util.LogInfo("wrote JS errors", zap.String("path", path), zap.Int("count", len(jsErrors)))
}

func (c *Crawler) writeWSMessages() {
	msgs := c.wsMessages.GetAll()
	if len(msgs) == 0 {
		return
	}
	wsMessages := make([]WSMsg, 0, len(msgs))
	for _, e := range msgs {
		if wm, ok := e.(WSMsg); ok {
			wsMessages = append(wsMessages, wm)
		}
	}
	if len(wsMessages) == 0 {
		return
	}
	data, err := json.MarshalIndent(wsMessages, "", "  ")
	if err != nil {
		util.LogError("failed to marshal WS messages", err)
		return
	}
	path := c.cfg.OutputDir + "/ws-messages.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write WS messages", err)
		return
	}
	util.LogInfo("wrote WS messages", zap.String("path", path), zap.Int("count", len(wsMessages)))
}
