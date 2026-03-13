package agentruntimemcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Missing file returns empty config
	cfg, err := LoadConfig("nonexistent.yaml")
	if err != nil {
		t.Fatalf("LoadConfig(nonexistent) should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return non-nil config")
	}

	// Create temp config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `
server:
  name: TestServer
  host: 0.0.0.0
  port: 9000
auth:
  mode: token
tracing:
  enabled: true
config:
  key:
    type: string
    required: true
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err = LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server == nil || cfg.Server.Name != "TestServer" {
		t.Errorf("expected server name TestServer, got %v", cfg.Server)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}
	if cfg.Tracing == nil || !cfg.Tracing.Enabled {
		t.Errorf("expected tracing enabled")
	}
}
