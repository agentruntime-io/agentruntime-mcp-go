package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// HeaderMCPInstanceID is the AgentRuntime → MCP header carrying the Control mcp_server_instances UUID.
// It is merged into POST /mcp/config runtime_context.instance_id. Matches agentruntime/mcp.HeaderMCPInstanceID.
const HeaderMCPInstanceID = "X-MCP-Instance-Id"

func fetchControlConfig(token string, configSchema map[string]any, runtimeContext map[string]any) (ConfigView, error) {
	base := strings.TrimSuffix(os.Getenv("MCP_CONTROL_SERVER_URL"), "/")
	if base == "" {
		return nil, nil
	}
	timeoutSec := 5
	if s := os.Getenv("MCP_CONTROL_TIMEOUT_SEC"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			timeoutSec = n
		}
	}
	payload := map[string]any{
		"configSchema":    configSchema,
		"config_schema":   configSchema,
		"schema":          configSchema,
		"runtimeContext":  runtimeContext,
		"runtime_context": runtimeContext,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrControlConfig, err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/mcp/config", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrControlConfig, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrControlConfig, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &ControlError{Status: resp.StatusCode, Body: string(bodyBytes)}
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("%w: invalid response: %v", ErrControlConfig, err)
	}
	if c, ok := data["config"].(map[string]any); ok {
		return ConfigView(c), nil
	}
	if d, ok := data["data"].(map[string]any); ok {
		return ConfigView(d), nil
	}
	return ConfigView(data), nil
}

func buildRuntimeContext(r *http.Request) map[string]any {
	ctx := make(map[string]any)
	if inst := strings.TrimSpace(r.Header.Get(HeaderMCPInstanceID)); inst != "" {
		ctx["instance_id"] = inst
	}
	if id := strings.TrimSpace(os.Getenv("MCP_SERVER_ID")); id != "" {
		ctx["server_id"] = id
	}
	if tn := r.Header.Get("X-Tool-Name"); tn != "" {
		ctx["tool_name"] = strings.TrimSpace(tn)
	} else if tn := r.Header.Get("X-MCP-Tool-Name"); tn != "" {
		ctx["tool_name"] = strings.TrimSpace(tn)
	} else {
		ctx["tool_name"] = "__initialize"
	}
	return ctx
}

// logRuntimeContextSummary helps debug Control POST /mcp/config validation (e.g. missing instance_id).
// - WARN when needConfig is true but runtime_context has neither instance_id nor server_id (likely 422 from Control).
// - DEBUG (MCP_LOG_LEVEL=debug): path, flags, tool_name, and X-MCP-Instance-Id header length (0 = absent).
func logRuntimeContextSummary(r *http.Request, ctx map[string]any, needConfig bool) {
	if r == nil || ctx == nil {
		return
	}
	_, hasInst := ctx["instance_id"]
	_, hasSrv := ctx["server_id"]
	tn, _ := ctx["tool_name"].(string)
	headerLen := len(strings.TrimSpace(r.Header.Get(HeaderMCPInstanceID)))

	if needConfig && !hasInst && !hasSrv {
		logWarn("mcp control config: missing instance_id and server_id in runtime_context (path=%s tool_name=%s); "+
			"send header %s or set MCP_SERVER_ID env", r.URL.Path, tn, HeaderMCPInstanceID)
	}
	logDebug("mcp control config: path=%s needConfig=%v instance_id_set=%v server_id_set=%v tool_name=%q %s_len=%d",
		r.URL.Path, needConfig, hasInst, hasSrv, tn, HeaderMCPInstanceID, headerLen)
}
