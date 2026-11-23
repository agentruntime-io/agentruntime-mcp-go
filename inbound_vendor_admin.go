package agentruntimemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// InboundVendorAdapter may be implemented alongside Adapter by connectors whose vendor webhook
// the platform registers for the person: POST register-webhook, GET webhook-status and POST
// unregister-webhook on the connector router. control-service calls these routes (the BFF no
// longer does). Implement it with MountInboundVendorRoutes, which owns authentication, request
// parsing and the response shapes; the connector supplies only the vendor calls.
type InboundVendorAdapter interface {
	RegisterInboundVendor(mux *http.ServeMux)
}

func inboundRoutePath(adapter, action, routeToken string) string {
	adapter = normalizeAdapterSlug(adapter)
	token := strings.Trim(strings.TrimSpace(routeToken), "/")
	if adapter == "" || token == "" {
		return ""
	}
	return fmt.Sprintf("/%s/inbound/%s/%s", adapter, action, token)
}

// InboundRegisterWebhookPath returns POST /{adapter}/inbound/register-webhook/{routeToken}.
func InboundRegisterWebhookPath(adapter, routeToken string) string {
	return inboundRoutePath(adapter, "register-webhook", routeToken)
}

// InboundWebhookStatusPath returns GET /{adapter}/inbound/webhook-status/{routeToken}.
func InboundWebhookStatusPath(adapter, routeToken string) string {
	return inboundRoutePath(adapter, "webhook-status", routeToken)
}

// InboundUnregisterWebhookPath returns POST /{adapter}/inbound/unregister-webhook/{routeToken}.
func InboundUnregisterWebhookPath(adapter, routeToken string) string {
	return inboundRoutePath(adapter, "unregister-webhook", routeToken)
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

// ResolveConfigError is a failed credential lookup through control's resolve-test.
type ResolveConfigError struct {
	// Status is control's HTTP status; zero when the answer was 2xx but held no configuration.
	Status int
	Body   string
	// Empty means control answered but the connection holds no credentials.
	Empty bool
}

func (e *ResolveConfigError) Error() string {
	if e.Empty {
		return "control resolve-test returned empty config_preview"
	}
	return fmt.Sprintf("control resolve-test: status %d: %s", e.Status, e.Body)
}

// IsConnectionUnavailable reports that the connection (or its credentials) no longer exists: control
// answered with a client error other than 408 or 429, or the connection holds no credentials. A
// control outage, a timeout or a network failure is not: those are worth retrying. Unregister uses
// it to tell "nothing left to remove" (answer 200, removed false) from "try again later" (502).
func IsConnectionUnavailable(err error) bool {
	var re *ResolveConfigError
	if !errors.As(err, &re) {
		return false
	}
	if re.Empty {
		return true
	}
	return re.Status >= 400 && re.Status < 500 && re.Status != http.StatusRequestTimeout && re.Status != http.StatusTooManyRequests
}

// ResolveMCPConnectionConfig resolves MCP connection credentials via Control resolve-test.
func ResolveMCPConnectionConfig(ctx context.Context, env TriggerEnv, tenantID, projectID, connectionID, serverID, toolName string) (ResolveTestResponse, error) {
	var out ResolveTestResponse
	tenantID = strings.TrimSpace(tenantID)
	connectionID = strings.TrimSpace(connectionID)
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)
	if tenantID == "" || connectionID == "" {
		return out, fmt.Errorf("tenant_id and connection_id required")
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
	bodyMap := map[string]any{
		"connection_id": connectionID,
		"tool_name":     toolName,
	}
	if serverID != "" {
		bodyMap["server_id"] = serverID
	}
	body, err := json.Marshal(bodyMap)
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
		return out, &ResolveConfigError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if len(out.ConfigPreview) == 0 {
		return out, &ResolveConfigError{Empty: true}
	}
	return out, nil
}

// ResolveMCPInstanceConfig resolves MCP instance credentials via Control resolve-test.
func ResolveMCPInstanceConfig(ctx context.Context, env TriggerEnv, tenantID, projectID, instanceID, toolName string) (ResolveTestResponse, error) {
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
		return out, &ResolveConfigError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if len(out.ConfigPreview) == 0 {
		return out, &ResolveConfigError{Empty: true}
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

// InboundVendorContext carries the tenant scope control sends with an admin call.
type InboundVendorContext struct {
	TenantID  string
	ProjectID string
}

// InboundVendorContextFromRequest reads the X-Tenant-Id / X-Project-Id headers control sets.
func InboundVendorContextFromRequest(r *http.Request) InboundVendorContext {
	return InboundVendorContext{
		TenantID:  strings.TrimSpace(r.Header.Get(inboundTenantHeader)),
		ProjectID: strings.TrimSpace(r.Header.Get(inboundProjectHeader)),
	}
}

// ValidateInboundInternalToken ensures the caller presents the fleet internal token.
func ValidateInboundInternalToken(r *http.Request, env TriggerEnv) error {
	expected := strings.TrimSpace(env.InternalToken)
	if expected == "" {
		return fmt.Errorf("MCP_CONTROL_INTERNAL_TOKEN not configured")
	}
	got := strings.TrimSpace(r.Header.Get(inboundInternalTokenHeader))
	if got == "" || !ConstantTimeEqual(got, expected) {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

// WriteInboundVendorJSON writes a JSON response for inbound vendor admin routes.
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
