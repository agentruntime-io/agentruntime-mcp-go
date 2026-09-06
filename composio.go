package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// ComposioExecutorMountPath is the shared MCP route for the Composio executor
	// (Control catalog rows with mode composio). Same platform layer as BridgeMountPath —
	// not a connectors/go-connectors package.
	ComposioExecutorMountPath = "/composio/mcp"
	composioAPIBase           = "https://backend.composio.dev/api/v3.1"
)

// HandlerForComposioExecutor returns the Composio executor HTTP handler (Option B REST).
func HandlerForComposioExecutor(configPath string) (http.Handler, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	var h http.Handler = http.HandlerFunc(composioHTTPHandler)
	h = ForwardIdentityHeaders(h)
	h = ForwardRequestBearer(h)
	return wrapWithTracing(cfg, h), nil
}

func composioHTTPHandler(w http.ResponseWriter, r *http.Request) {
	mount := ComposioExecutorMountPath
	if r.Method == http.MethodGet && isSchemaEndpointForMount(r, mount) {
		serveComposioSchema(w, r)
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
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, composioControlErr(err)))
		return
	}
	composioMeta, err := composioMetaFromPayload(payload)
	if err != nil {
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, err.Error()))
		return
	}

	switch method {
	case "tools/list":
		result := map[string]any{"tools": composioPublishedTools(composioMeta)}
		writeJSONRPCResponse(w, reqID, result, nil)
	case "tools/call":
		result, callErr := composioCallTool(payload, composioMeta, data)
		if callErr != nil {
			writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32603, callErr.Error()))
			return
		}
		writeJSONRPCResponse(w, reqID, result, nil)
	default:
		writeJSONRPCResponse(w, reqID, nil, jsonRPCErr(-32601, "Method not found: "+method))
	}
}

func serveComposioSchema(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	ctx := buildRuntimeContext(r)
	payload, err := fetchControlPayload(token, map[string]any{}, ctx)
	if err != nil {
		http.Error(w, composioControlErr(err), http.StatusBadGateway)
		return
	}
	schema := payload.ConfigSchema
	if schema == nil {
		schema = map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schema)
}

func composioMetaFromPayload(payload *ControlPayload) (map[string]any, error) {
	if payload == nil || payload.Composio == nil {
		return nil, fmt.Errorf("server is not configured for composio mode (missing metadata.composio)")
	}
	toolkit := strings.TrimSpace(stringFromMap(payload.Composio, "toolkit"))
	if toolkit == "" {
		return nil, fmt.Errorf("composio toolkit is not set on mcp_servers.metadata")
	}
	return payload.Composio, nil
}

func composioPublishedTools(meta map[string]any) []map[string]any {
	raw, ok := meta["published_tools"].([]any)
	if !ok || len(raw) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		t, ok := item.(map[string]any)
		if !ok || len(t) == 0 {
			continue
		}
		name := strings.TrimSpace(stringFromMap(t, "name"))
		if name == "" {
			continue
		}
		tool := map[string]any{
			"name":        name,
			"description": strings.TrimSpace(stringFromMap(t, "description")),
		}
		if schema, ok := t["inputSchema"].(map[string]any); ok && len(schema) > 0 {
			tool["inputSchema"] = schema
		} else if schema, ok := t["input_schema"].(map[string]any); ok && len(schema) > 0 {
			tool["inputSchema"] = schema
		}
		out = append(out, tool)
	}
	return out
}

func composioCallTool(payload *ControlPayload, meta map[string]any, data map[string]any) (map[string]any, error) {
	toolName, ok := toolNameFromParams(data)
	if !ok {
		return nil, fmt.Errorf("tools/call missing params.name")
	}
	slug, err := composioSlugForTool(meta, toolName)
	if err != nil {
		return nil, err
	}
	if !composioAllowlisted(meta, slug) {
		return nil, fmt.Errorf("tool %q is not in composio allowlist", toolName)
	}

	apiKey := strings.TrimSpace(stringFromMap(payload.Config, "composio_api_key"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(stringFromMap(payload.Config, "COMPOSIO_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("missing composio_api_key in instance config")
	}
	userID := strings.TrimSpace(stringFromMap(payload.Config, "composio_user_id"))
	if userID == "" {
		return nil, fmt.Errorf("missing composio_user_id in instance config")
	}

	args, _ := data["params"].(map[string]any)
	arguments := map[string]any{}
	if args != nil {
		if a, ok := args["arguments"].(map[string]any); ok {
			arguments = a
		} else {
			// MCP SDK sends tool fields at top level of params (excluding name).
			for k, v := range args {
				if k == "name" || k == "_meta" {
					continue
				}
				arguments[k] = v
			}
		}
	}

	execBody := map[string]any{
		"user_id":   userID,
		"arguments": arguments,
	}
	body, err := json.Marshal(execBody)
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(composioAPIBase, "/") + "/tools/execute/" + slug
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("composio execute: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("composio execute HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("composio execute invalid JSON: %w", err)
	}
	return composioMCPResult(parsed), nil
}

func composioSlugForTool(meta map[string]any, toolName string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "", fmt.Errorf("empty tool name")
	}
	if raw, ok := meta["published_tools"].([]any); ok {
		for _, item := range raw {
			t, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(stringFromMap(t, "name"))
			if !strings.EqualFold(name, toolName) {
				continue
			}
			if slug := strings.TrimSpace(stringFromMap(t, "composio_slug")); slug != "" {
				return slug, nil
			}
			if md, ok := t["metadata"].(map[string]any); ok {
				if slug := strings.TrimSpace(stringFromMap(md, "composio_slug")); slug != "" {
					return slug, nil
				}
			}
		}
	}
	return strings.ToUpper(strings.ReplaceAll(toolName, "-", "_")), nil
}

func composioAllowlisted(meta map[string]any, slug string) bool {
	raw, ok := meta["tool_allowlist"].([]any)
	if !ok || len(raw) == 0 {
		return true
	}
	slug = strings.ToUpper(strings.TrimSpace(slug))
	for _, item := range raw {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item)), slug) {
			return true
		}
	}
	return false
}

func composioMCPResult(composioResp map[string]any) map[string]any {
	successful, _ := composioResp["successful"].(bool)
	errMsg := strings.TrimSpace(stringFromMap(composioResp, "error"))
	if !successful && errMsg != "" {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": errMsg},
			},
			"isError": true,
		}
	}
	data := composioResp["data"]
	if data == nil {
		data = composioResp
	}
	b, _ := json.Marshal(data)
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(b)},
		},
		"structuredContent": data,
	}
}

func composioControlErr(err error) string {
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
