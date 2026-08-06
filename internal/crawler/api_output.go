package crawler

import (
	"encoding/json"
	"os"

	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

func apiURLMatches(rawURL string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if queue.URLGlobMatch(p, rawURL) {
			return true
		}
	}
	return false
}

func (c *Crawler) writeAPIResponses(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}
	data, err := json.MarshalIndent(apiResp, "", "  ")
	if err != nil {
		util.LogError("failed to marshal API responses", err)
		return
	}
	path := c.cfg.OutputDir + "/api-responses.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write API responses", err)
		return
	}
	util.LogInfo("wrote API responses", zap.String("path", path), zap.Int("count", len(apiResp)))
}

func extractGraphQLOp(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var gqlReq struct {
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(body, &gqlReq); err != nil {
		return ""
	}
	return gqlReq.OperationName
}
