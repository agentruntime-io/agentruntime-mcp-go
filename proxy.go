package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProxyConfig holds proxy target and overlay settings.
type ProxyConfig struct {
	TargetURL   string
	OverlayFile string
	BearerToken string
	Host        string
	Port        int
}

// BuildProxyApp creates proxy configuration. The proxy intercepts tools/list and tools/call
// at the HTTP layer; no MCP tools are registered.
func BuildProxyApp(targetURL, overlayFile, bearerToken string) (*proxyClient, error) {
	return &proxyClient{
		targetURL:   targetURL,
		overlayFile: overlayFile,
		bearerToken: bearerToken,
	}, nil
}

type proxyClient struct {
	targetURL   string
	overlayFile string
	bearerToken string
}

func (p *proxyClient) post(payload map[string]any) (map[string]any, error) {
	if p.targetURL == "" {
		return nil, ErrProxyTarget
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	req, err := http.NewRequest(http.MethodPost, p.targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProxyTarget, err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if p.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.bearerToken)
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

func (p *proxyClient) loadOverlay() map[string]any {
	if p.overlayFile == "" {
		return nil
	}
	data, err := os.ReadFile(p.overlayFile)
	if err != nil {
		logDebug("overlay file read failed: %v", err)
		return nil
	}
	var ov struct {
		Tools []map[string]any `yaml:"tools"`
	}
	if err := yaml.Unmarshal(data, &ov); err != nil {
		logDebug("overlay yaml parse failed: %v", err)
		return nil
	}
	out := make(map[string]any)
	for _, t := range ov.Tools {
		if name, ok := t["name"].(string); ok {
			out[name] = t
		}
	}
	return out
}

func (p *proxyClient) fetchAndMergeTools() ([]any, error) {
	res, err := p.post(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	result, _ := res["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if tools == nil {
		tools = []any{}
	}

	overlay := p.loadOverlay()
	if overlay != nil {
		merged := make([]any, 0, len(tools))
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				merged = append(merged, t)
				continue
			}
			name, _ := tm["name"].(string)
			if o, ok := overlay[name].(map[string]any); ok {
				mt := make(map[string]any)
				for k, v := range tm {
					mt[k] = v
				}
				if desc, ok := o["description"].(string); ok && desc != "" {
					mt["description"] = desc
				}
				if schema, ok := o["inputSchema"]; ok && schema != nil {
					mt["inputSchema"] = schema
				}
				if schema, ok := o["outputSchema"]; ok && schema != nil {
					mt["outputSchema"] = schema
				}
				merged = append(merged, mt)
			} else {
				merged = append(merged, t)
			}
		}
		tools = merged
	}
	return tools, nil
}

func writeJSONRPCResponse(w http.ResponseWriter, id any, result map[string]any, rpcErr *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) {
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if rpcErr != nil {
		body = map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcErr}
	} else {
		body = map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
	}
	json.NewEncoder(w).Encode(body)
}

// RunProxy starts the proxy server. Blocks until the server exits.
// Intercepts tools/list (returns merged tools) and tools/call (forwards to target).
// Other MCP methods are forwarded to the target.
func RunProxy(targetURL, overlayFile, bearerToken, host string, port int) error {
	if targetURL == "" {
		return ErrProxyTarget
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 8010
	}

	proxy, err := BuildProxyApp(targetURL, overlayFile, bearerToken)
	if err != nil {
		return err
	}

	addr := host + ":" + strconv.Itoa(port)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/mcp/config/schema" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONRPCResponse(w, nil, nil, &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{-32700, "Parse error"})
			return
		}
		r.Body.Close()

		var data map[string]any
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			writeJSONRPCResponse(w, nil, nil, &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{-32700, "Parse error"})
			return
		}

		reqID := data["id"]
		method, _ := data["method"].(string)

		if method == "tools/list" {
			tools, err := proxy.fetchAndMergeTools()
			if err != nil {
				writeJSONRPCResponse(w, reqID, nil, &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{-32603, err.Error()})
				return
			}
			writeJSONRPCResponse(w, reqID, map[string]any{"tools": tools}, nil)
			return
		}

		if method == "tools/call" {
			params, _ := data["params"].(map[string]any)
			if params == nil {
				params = map[string]any{}
			}
			name, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			res, err := proxy.post(map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/call",
				"params":  map[string]any{"name": name, "arguments": args},
			})
			if err != nil {
				writeJSONRPCResponse(w, reqID, nil, &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{-32603, err.Error()})
				return
			}
			result, _ := res["result"].(map[string]any)
			if result == nil {
				result = map[string]any{}
			}
			writeJSONRPCResponse(w, reqID, result, nil)
			return
		}

		// Forward all other methods (initialize, notifications/initialized, etc.) to target
		res, err := proxy.post(data)
		if err != nil {
			writeJSONRPCResponse(w, reqID, nil, &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{-32603, err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})

	log.Printf("MCP proxy listening on %s -> %s", addr, targetURL)
	return http.ListenAndServe(addr, handler)
}
