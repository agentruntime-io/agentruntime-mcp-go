package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// BridgeMountPath is the single generic route for all Control-registered external MCP bridges.
// Register mcp_servers.canonical_url to https://<router-host>/bridge/mcp (same path for every bridged server).
// Per-server upstream URL and auth mapping live in mcp_servers.metadata (see docs/mcp/MCP_BRIDGE.md).
const BridgeMountPath = "/bridge/mcp"

// HandlerForBridge returns the generic MCP bridge HTTP handler.
func HandlerForBridge(configPath string) (http.Handler, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	var h http.Handler = http.HandlerFunc(bridgeHTTPHandler)
	h = ForwardIdentityHeaders(h)
	h = ForwardRequestBearer(h)
	return wrapWithTracing(cfg, h), nil
}

func bridgeHTTPHandler(w http.ResponseWriter, r *http.Request) {
	mount := BridgeMountPath
	if r.Method == http.MethodGet && isSchemaEndpointForMount(r, mount) {
		serveBridgeSchema(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONRPCResponse(w, nil, nil, jsonRPCErr(-32700, "Parse error"))
		return
	}
	_ = r.Body.Close()

	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		writeJSONRPCResponse(w, nil, nil, jsonRPCErr(-32700, "Parse error"))
		return
	}
	reqID := data["id"]
	method, _ := data["method"].(string)

	ctx := buildRuntimeContext(r)
	if method == "tools/call" {
		if tn, ok := toolNameFromParams(data); ok {
			ctx["tool_name"] = tn
		}
	}

	token := extractToken(r)
	payload, err := fetchControlPayload(token, map[string]any{}, ctx)
	if err != nil {
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, bridgeControlErr(err)))
		return
	}
	upstream, err := bridgeUpstreamFromPayload(payload)
	if err != nil {
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, err.Error()))
		return
	}

	authHdr, err := ApplyBridgeHeaders(payload.Config, payload.Bridge)
	if err != nil {
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, "upstream auth: "+err.Error()))
		return
	}

	switch method {
	case "tools/list":
		res, err := bridgePost(upstream, data, authHdr)
		if err != nil {
			writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, err.Error()))
			return
		}
		result, _ := res["result"].(map[string]any)
		if result == nil {
			result = map[string]any{}
		}
		writeJSONRPCResponse(w, reqID, result, nil)
	case "tools/call":
		res, err := bridgePost(upstream, data, authHdr)
		if err != nil {
			writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, err.Error()))
			return
		}
		result, _ := res["result"].(map[string]any)
		if result == nil {
			result = map[string]any{}
		}
		writeJSONRPCResponse(w, reqID, result, nil)
	default:
		res, err := bridgePost(upstream, data, authHdr)
		if err != nil {
			writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	}
}

func serveBridgeSchema(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	ctx := buildRuntimeContext(r)
	payload, err := fetchControlPayload(token, map[string]any{}, ctx)
	if err != nil {
		http.Error(w, bridgeControlErr(err), http.StatusBadGateway)
		return
	}
	schema := payload.ConfigSchema
	if schema == nil {
		schema = map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schema)
}

func bridgeUpstreamFromPayload(payload *ControlPayload) (string, error) {
	if payload == nil || payload.Bridge == nil {
		return "", fmt.Errorf("server is not configured for bridge mode (missing metadata.bridge upstream_url)")
	}
	upstream := strings.TrimSpace(stringFromMap(payload.Bridge, "upstream_url"))
	if upstream == "" {
		return "", fmt.Errorf("bridge upstream_url is not set on mcp_servers.metadata")
	}
	return upstream, nil
}

func bridgePost(targetURL string, payload map[string]any, extra http.Header) (map[string]any, error) {
	if strings.TrimSpace(targetURL) == "" {
		return nil, ErrProxyTarget
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	for k, vals := range extra {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: target returned %d: %s", ErrProxyTarget, resp.StatusCode, string(b))
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	return result, nil
}

func toolNameFromParams(data map[string]any) (string, bool) {
	params, _ := data["params"].(map[string]any)
	if params == nil {
		return "", false
	}
	name, _ := params["name"].(string)
	name = strings.TrimSpace(name)
	return name, name != ""
}

func bridgeControlErr(err error) string {
	if err == nil {
		return "control error"
	}
	if ce, ok := err.(*ControlError); ok {
		if hm := HumanMessageFromControlAPIBody(ce.Body); hm != "" {
			return hm
		}
	}
	return err.Error()
}

func jsonRPCErr(code int, msg string) *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
} {
	return &struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, msg}
}
