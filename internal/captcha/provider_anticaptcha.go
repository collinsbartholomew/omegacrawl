package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (s *Solver) pollAntiCaptcha(ctx context.Context, taskID string) (*SolveResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

	first := true
	for {
		if !first {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pollTick.C:
			}
		}
		first = false

		requestBody := map[string]interface{}{
			"clientKey": s.apiKey,
			"taskId":    taskID,
		}
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal poll request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/getTaskResult", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("provider API error (status %d): %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if errorID, ok := result["errorId"].(float64); ok && errorID != 0 {
			return nil, fmt.Errorf("provider error %d", int(errorID))
		}

		if status, ok := result["status"].(string); ok {
			switch status {
			case "ready":
				token, captchaID := extractSolution(result)
				if token == "" {
					return nil, fmt.Errorf("task finished without a solution: %v", result)
				}
				return &SolveResponse{Token: token, CaptchaID: captchaID, Solved: true}, nil
			case "processing":
				continue
			default:
				return nil, fmt.Errorf("task failed with status: %v", status)
			}
		}

		return nil, fmt.Errorf("unexpected response: %v", result)
	}
}
