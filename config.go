package agentruntimemcp

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServerConfig is the parsed config.yaml structure.
type ServerConfig struct {
	Server  *ServerSection  `yaml:"server"`
	Auth    *AuthSection   `yaml:"auth"`
	Tracing *TracingSection `yaml:"tracing"`
	Config  map[string]any `yaml:"config"`
}

type ServerSection struct {
	Name          string `yaml:"name"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	StatelessHTTP bool   `yaml:"stateless_http"`
}

type AuthSection struct {
	Mode string `yaml:"mode"`
}

type TracingSection struct {
	Enabled bool `yaml:"enabled"`
}

// LoadConfig loads config from a YAML file. Returns empty config if file not found.
func LoadConfig(path string) (*ServerConfig, error) {
	if path == "" {
		path = "config.yaml"
	}
	if !filepath.IsAbs(path) {
		cwd, _ := os.Getwd()
		path = filepath.Join(cwd, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServerConfig{}, nil
		}
		return nil, err
	}
	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
