package agentruntimemcp

import (
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Adapter registers tools and config schema with an MCP server.
// Implement this interface for each connector. Use RegisterAdapter in init().
type Adapter interface {
	Register(server *mcp.Server, schema SchemaWriter)
}

// WebhookAdapter may be implemented alongside Adapter by connectors that receive
// vendor webhooks (GitHub, Slack, Stripe, etc.) and forward them as signed ingress
// requests on the BFF (POST /v1/inbound-webhooks/{subscription_id}; formerly "Mode B" in docs).
//
// RunWithRouter calls RegisterWebhook for every adapter that implements this interface,
// passing the shared HTTP mux so the adapter can register its own plain-HTTP route
// (e.g. mux.HandleFunc("/github/webhook", ...)) alongside the MCP route.
//
// Implementing this interface is entirely optional — adapters that only expose MCP
// tools are not affected.
//
// Inside the registered handler, use SignModeB / DeliverModeB to forward the
// verified vendor payload to the BFF as a canonical signed ingress delivery.
type WebhookAdapter interface {
	RegisterWebhook(mux *http.ServeMux)
}

// AdapterConstructorInput is passed to adapter constructors. Reserved for future use.
type AdapterConstructorInput struct{}

// AdapterConstructor creates an Adapter instance. Used with RegisterAdapter.
type AdapterConstructor func(AdapterConstructorInput) (Adapter, error)

var registry = struct {
	mu       sync.RWMutex
	adapters map[string]AdapterConstructor
}{
	adapters: make(map[string]AdapterConstructor),
}

// RegisterAdapter registers an adapter by name. Call from init() so the adapter
// is available when RunWithRegistry runs. Like foodics/worker-go plugin pattern.
func RegisterAdapter(name string, ctor AdapterConstructor) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.adapters[name] = ctor
}

// ListAdapterNames returns all registered adapter names.
func ListAdapterNames() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.adapters))
	for k := range registry.adapters {
		names = append(names, k)
	}
	return names
}

// getAdapters returns adapters by name. If names is empty, returns all.
func getAdapters(names []string) ([]Adapter, error) {
	registry.mu.RLock()
	ctors := make(map[string]AdapterConstructor)
	for k, v := range registry.adapters {
		ctors[k] = v
	}
	registry.mu.RUnlock()

	input := AdapterConstructorInput{}
	var result []Adapter
	if len(names) == 0 {
		for _, ctor := range ctors {
			adp, err := ctor(input)
			if err != nil {
				return nil, err
			}
			result = append(result, adp)
		}
	} else {
		for _, name := range names {
			ctor, ok := ctors[name]
			if !ok {
				return nil, &ErrAdapterNotFound{Name: name}
			}
			adp, err := ctor(input)
			if err != nil {
				return nil, err
			}
			result = append(result, adp)
		}
	}
	return result, nil
}
