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

// ControlCallback notifies Control when MCP relay instances connect or disconnect.
type ControlCallback struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewControlCallback(baseURL, token string) *ControlCallback {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &ControlCallback{
		BaseURL: baseURL,
		Token:   strings.TrimSpace(token),
		Client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *ControlCallback) NotifyInstanceConnected(ctx context.Context, instanceID, tenantID, canonicalURL string) {
	if c == nil || c.BaseURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{
		"instance_id":    instanceID,
		"tenant_id":      tenantID,
		"canonical_url":  canonicalURL,
		"connectivity":   "online",
	})
	_ = c.post(ctx, "/internal/mcp/relay/instance-connected", body)
}

func (c *ControlCallback) NotifyInstanceDisconnected(ctx context.Context, instanceID, tenantID, reason string) {
	if c == nil || c.BaseURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{
		"instance_id":  instanceID,
		"tenant_id":    tenantID,
		"reason":       reason,
		"connectivity": "offline",
	})
	_ = c.post(ctx, "/internal/mcp/relay/instance-disconnected", body)
}

func (c *ControlCallback) post(ctx context.Context, path string, body []byte) error {
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
		return fmt.Errorf("control callback %s: %s (%s)", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
