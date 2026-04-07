package agentruntimemcp

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Adapter registers tools and config schema with an MCP server.
// Implement this interface for each connector. Use RegisterAdapter in init().
type Adapter interface {
	Register(server *mcp.Server, schema SchemaWriter)
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
