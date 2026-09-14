package agentruntimemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ServiceConnectorRouter is the X-Service-Name value for connector resolve-test calls.
	ServiceConnectorRouter = "connector-router"

	inboundInternalTokenHeader = "X-Internal-Token"
	inboundTenantHeader        = "X-Tenant-Id"
	inboundProjectHeader       = "X-Project-Id"
)

// InboundVendorAdapter may be implemented alongside Adapter by connectors that support
// platform-managed vendor webhook registration (POST register / GET status on the connector router).
type InboundVendorAdapter interface {
	RegisterInboundVendor(mux *http.ServeMux)
}

// InboundRegisterWebhookPath returns POST /{adapter}/inbound/register-webhook/{routeToken}.
func InboundRegisterWebhookPath(adapter, routeToken string) string {
	adapter = strings.Trim(strings.ToLower(strings.TrimSpace(adapter)), "/")
	token := strings.Trim(strings.TrimSpace(routeToken), "/")
	if adapter == "" || token == "" {
		return ""
	}
	return fmt.Sprintf("/%s/inbound/register-webhook/%s", adapter, token)
}

// InboundWebhookStatusPath returns GET /{adapter}/inbound/webhook-status/{routeToken}.
func InboundWebhookStatusPath(adapter, routeToken string) string {
	adapter = strings.Trim(strings.ToLower(strings.TrimSpace(adapter)), "/")
	token := strings.Trim(strings.TrimSpace(routeToken), "/")
	if adapter == "" || token == "" {
		return ""
	}
	return fmt.Sprintf("/%s/inbound/webhook-status/%s", adapter, token)
}

// InboundVendorMountPrefix returns /{adapter}/inbound/ for mux registration.
func InboundVendorMountPrefix(adapter string) string {
	adapter = strings.Trim(strings.ToLower(strings.TrimSpace(adapter)), "/")
	if adapter == "" {
		return ""
	}
	return fmt.Sprintf("/%s/inbound/", adapter)
}

// ResolveTestResponse is the subset of POST /v1/mcp/config/resolve-test used by inbound vendor flows.
type ResolveTestResponse struct {
	ConfigPreview map[string]any `json:"config_preview"`
	Meta          map[string]any `json:"meta"`
}

// ResolveMCPInstanceConfig resolves MCP instance credentials via Control resolve-test.
func ResolveMCPInstanceConfig(ctx context.Context, env InboundAdapterRouteEnv, tenantID, projectID, instanceID, toolName string) (ResolveTestResponse, error) {
	var out ResolveTestResponse
	tenantID = strings.TrimSpace(tenantID)
	instanceID = strings.TrimSpace(instanceID)
	toolName = strings.TrimSpace(toolName)
	if tenantID == "" || instanceID == "" {
		return out, fmt.Errorf("tenant_id and mcp_instance_id required")
	}
	if env.ControlURL == "" || env.InternalToken == "" {
		return out, fmt.Errorf("MCP_CONTROL_SERVER_URL and MCP_CONTROL_INTERNAL_TOKEN required")
	}
	if toolName == "" {
		toolName = "resolve_test"
	}
	q := url.Values{}
	q.Set("tenant_id", tenantID)
	if projectID = strings.TrimSpace(projectID); projectID != "" {
		q.Set("project_id", projectID)
	}
	path := env.ControlURL + "/v1/mcp/config/resolve-test?" + q.Encode()
	body, err := json.Marshal(map[string]any{
		"instance_id": instanceID,
		"tool_name":   toolName,
	})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(inboundInternalTokenHeader, env.InternalToken)
	req.Header.Set("X-Service-Name", ServiceConnectorRouter)
	req.Header.Set(inboundTenantHeader, tenantID)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("control resolve-test: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if len(out.ConfigPreview) == 0 {
		return out, fmt.Errorf("control resolve-test returned empty config_preview")
	}
	return out, nil
}

// InboundRouteToken extracts route_token from /{adapter}/inbound/{action}/{routeToken}.
func InboundRouteToken(path, mountPrefix, action string) string {
	path = strings.TrimSpace(path)
	prefix := strings.TrimRight(strings.TrimSpace(mountPrefix), "/") + "/" + strings.Trim(strings.TrimSpace(action), "/") + "/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(path, prefix), "/")
}

// WriteInboundVendorJSON writes a JSON response for inbound vendor admin routes.
type InboundVendorContext struct {
	TenantID  string
	ProjectID string
}

// InboundVendorContextFromRequest reads tenant/project headers set by the BFF proxy.
func InboundVendorContextFromRequest(r *http.Request) InboundVendorContext {
	return InboundVendorContext{
		TenantID:  strings.TrimSpace(r.Header.Get(inboundTenantHeader)),
		ProjectID: strings.TrimSpace(r.Header.Get(inboundProjectHeader)),
	}
}

// ValidateInboundInternalToken ensures the caller presents the fleet internal token.
func ValidateInboundInternalToken(r *http.Request, env InboundAdapterRouteEnv) error {
	expected := strings.TrimSpace(env.InternalToken)
	if expected == "" {
		return fmt.Errorf("MCP_CONTROL_INTERNAL_TOKEN not configured")
	}
	got := strings.TrimSpace(r.Header.Get(inboundInternalTokenHeader))
	if got == "" || got != expected {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

// InboundVendorContext carries tenant scope from BFF proxy headers.
func WriteInboundVendorJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// WriteInboundVendorError writes a standard error JSON body.
func WriteInboundVendorError(w http.ResponseWriter, status int, msg string) {
	WriteInboundVendorJSON(w, status, map[string]string{"error": msg})
}

// NormalizeStringList deduplicates and trims string slices.
func NormalizeStringList(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
