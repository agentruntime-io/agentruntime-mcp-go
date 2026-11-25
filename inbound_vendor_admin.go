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
	// Code and Message come from control's error envelope ({"error": code, "message": text}).
	Code    string
	Message string
	// Empty means control answered but the connection holds no credentials.
	Empty bool
}

func newResolveConfigError(status int, body string) *ResolveConfigError {
	e := &ResolveConfigError{Status: status, Body: body}
	var env struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &env) == nil {
		e.Code, e.Message = env.Error, env.Message
	}
	return e
}

func (e *ResolveConfigError) Error() string {
	if e.Empty {
		return "control resolve-test returned empty config_preview"
	}
	return fmt.Sprintf("control resolve-test: status %d: %s", e.Status, e.Body)
}

// IsConnectionUnavailable reports that the connection itself no longer exists or holds no credentials:
//   - control answered but the connection has no configuration (empty config_preview);
//   - 404 with the code connection_not_found;
//   - 422 "missing required config field" (a deleted connection resolves to no value for a required field).
//
// Everything else is NOT "gone": a wrong internal token (401), a service name control does not accept
// (403 auth_forbidden), a malformed call (400), a hidden tool or missing server (404 not_found), a timeout,
// rate limit, outage or network failure. Those are wiring or availability faults; reading them as "gone"
// would let a last-trigger delete answer "nothing left to remove" and make control drop the ingress with
// the vendor webhooks still registered. Unregister uses this to tell "nothing left to remove" (200,
// removed false) from "try again later" (502).
func IsConnectionUnavailable(err error) bool {
	var re *ResolveConfigError
	if !errors.As(err, &re) {
		return false
	}
	if re.Empty {
		return true
	}
	switch {
	case re.Status == http.StatusNotFound && re.Code == "connection_not_found":
		return true
	case re.Status == http.StatusUnprocessableEntity && re.Code == "validation_error" &&
		strings.Contains(strings.ToLower(re.Message), "missing required config field"):
		return true
	}
	return false
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
		return out, newResolveConfigError(resp.StatusCode, strings.TrimSpace(string(raw)))
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
		return out, newResolveConfigError(resp.StatusCode, strings.TrimSpace(string(raw)))
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
