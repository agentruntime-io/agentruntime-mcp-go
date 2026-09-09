package agentruntimemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// InboundAdapterRouteEnv holds fleet env for connector adapter route lookup.
type InboundAdapterRouteEnv struct {
	WheelhouseURL    string
	InternalToken    string
	BFFBaseURL       string
	ConnectorBaseURL string
}

// LoadInboundAdapterRouteEnv reads connector/router env for adapter ingress.
func LoadInboundAdapterRouteEnv() InboundAdapterRouteEnv {
	return InboundAdapterRouteEnv{
		WheelhouseURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("GO_WHEELHOUSE_URL")), "/"),
		InternalToken:    strings.TrimSpace(os.Getenv("GO_WHEELHOUSE_INTERNAL_TOKEN")),
		BFFBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTRUNTIME_BFF_BASE_URL")), "/"),
		ConnectorBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("RELAY_PUBLIC_BASE_URL")), "/"),
	}
}

// InboundAdapterRouteCredentials is returned by Wheelhouse internal lookup.
type InboundAdapterRouteCredentials struct {
	SubscriptionID string `json:"subscription_id"`
	SigningSecret  string `json:"signing_secret"`
	VendorSecret   string `json:"vendor_secret"`
	Adapter        string `json:"adapter"`
	TenantID       string `json:"tenant_id"`
	Enabled        bool   `json:"enabled"`
}

// LookupInboundAdapterRoute resolves route_token via Wheelhouse internal API.
func LookupInboundAdapterRoute(ctx context.Context, env InboundAdapterRouteEnv, routeToken string) (*InboundAdapterRouteCredentials, error) {
	routeToken = strings.TrimSpace(routeToken)
	if routeToken == "" {
		return nil, fmt.Errorf("route_token required")
	}
	if env.WheelhouseURL == "" || env.InternalToken == "" {
		return nil, fmt.Errorf("GO_WHEELHOUSE_URL and GO_WHEELHOUSE_INTERNAL_TOKEN required for adapter route lookup")
	}
	path := fmt.Sprintf("%s/internal/workflow-webhooks/inbound/adapter-routes/%s/credentials",
		env.WheelhouseURL, url.PathEscape(routeToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Internal-Token", env.InternalToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wheelhouse adapter route lookup: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out InboundAdapterRouteCredentials
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if !out.Enabled || strings.TrimSpace(out.SigningSecret) == "" {
		return nil, nil
	}
	return &out, nil
}

// ResolveInboundForwardTargets picks BFF base URL and credentials for adapter forwarding.
func ResolveInboundForwardTargets(env InboundAdapterRouteEnv, creds *InboundAdapterRouteCredentials) (bffBaseURL string, subscriptionID, signingSecret string, err error) {
	if creds == nil {
		return "", "", "", fmt.Errorf("adapter route not found")
	}
	bff := strings.TrimSpace(env.BFFBaseURL)
	if bff == "" {
		return "", "", "", fmt.Errorf("AGENTRUNTIME_BFF_BASE_URL required")
	}
	return bff, creds.SubscriptionID, creds.SigningSecret, nil
}

// ConnectorWebhookPath returns the public path segment for an adapter route.
func ConnectorWebhookPath(adapter, routeToken string) string {
	adapter = strings.Trim(strings.ToLower(strings.TrimSpace(adapter)), "/")
	token := strings.Trim(strings.TrimSpace(routeToken), "/")
	if adapter == "" || token == "" {
		return ""
	}
	return fmt.Sprintf("/%s/webhook/%s", adapter, token)
}

// ConnectorWebhookURL builds the full public connector URL when RELAY_PUBLIC_BASE_URL is set.
func ConnectorWebhookURL(env InboundAdapterRouteEnv, adapter, routeToken string) string {
	base := strings.TrimRight(strings.TrimSpace(env.ConnectorBaseURL), "/")
	path := ConnectorWebhookPath(adapter, routeToken)
	if base == "" || path == "" {
		return ""
	}
	return base + path
}
