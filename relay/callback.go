package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OrchestratorCallback notifies agentruntime when harness workers reconnect or runs complete.
type OrchestratorCallback struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewOrchestratorCallback(baseURL, token string) *OrchestratorCallback {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &OrchestratorCallback{
		BaseURL: baseURL,
		Token:   strings.TrimSpace(token),
		Client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *OrchestratorCallback) NotifyWorkerOnline(ctx context.Context, workerID, tenantID, userID string) {
	if c == nil || c.BaseURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{
		"worker_id": workerID,
		"tenant_id": tenantID,
		"user_id":   userID,
	})
	_ = c.post(ctx, "/api/v1/internal/harness/worker-online", body)
}

func (c *OrchestratorCallback) NotifyRunComplete(ctx context.Context, runID, status string, result map[string]any, errMsg string) {
	if c == nil || c.BaseURL == "" {
		return
	}
	payload := map[string]any{
		"run_id": runID,
		"status": status,
	}
	if result != nil {
		payload["result"] = result
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	body, _ := json.Marshal(payload)
	_ = c.post(ctx, "/api/v1/internal/harness/run-complete", body)
}

func (c *OrchestratorCallback) post(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("X-Internal-Token", c.Token)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("harness callback %s: %s (%s)", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
