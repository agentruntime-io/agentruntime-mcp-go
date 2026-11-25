package agentruntimemcp

import (
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type registryManifestAdapter struct{ m TriggerManifest }

func (registryManifestAdapter) Register(*mcp.Server, SchemaWriter) {}
func (a registryManifestAdapter) TriggerManifest() TriggerManifest { return a.m }
func (registryManifestAdapter) RegisterWebhook(*http.ServeMux)     {}

type registryPlainAdapter struct{}

func (registryPlainAdapter) Register(*mcp.Server, SchemaWriter) {}

func TestRegisteredTriggerManifests_onlyAdaptersThatDeclareOne(t *testing.T) {
	RegisterAdapter("registry-test-declares", func(AdapterConstructorInput) (Adapter, error) {
		return registryManifestAdapter{m: validTestManifest()}, nil
	})
	RegisterAdapter("registry-test-plain", func(AdapterConstructorInput) (Adapter, error) {
		return registryPlainAdapter{}, nil
	})
	got, err := RegisteredTriggerManifests()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got["registry-test-declares"]
	if !ok || m.Label != "Telegram" {
		t.Fatalf("manifests = %v", got)
	}
	if _, ok := got["registry-test-plain"]; ok {
		t.Fatal("an adapter without a manifest must not appear")
	}
}
