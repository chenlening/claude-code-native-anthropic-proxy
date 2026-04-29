package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigFromFile(t *testing.T) {
	content := `
server:
  listen: ":8080"
  read_timeout: 30s
  write_timeout: 120s
  idle_timeout: 90s

logging:
  level: info
  format: json

metrics:
  enabled: true
  path: /metrics

health:
  path: /health

routing:
  default_strategy: least-connections

models:
  claude-sonnet-4-20250514:
    backends:
      - endpoint: endpoint-a
        model: "claude-sonnet-4-20250514"
        weight: 10

endpoints:
  endpoint-a:
    url: "https://api.anthropic.com"
    api_key: "test-key"
    timeout: 90s

endpoint_health:
  failures_to_disable: 5
  recovery_probe_interval: 30s
  successes_to_enable: 2
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString(content)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify server config
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 30s", cfg.Server.ReadTimeout)
	}

	// Verify endpoints
	if len(cfg.Endpoints) != 1 {
		t.Errorf("len(Endpoints) = %d, want 1", len(cfg.Endpoints))
	}
	if cfg.Endpoints["endpoint-a"].URL != "https://api.anthropic.com" {
		t.Errorf("endpoint-a URL = %q", cfg.Endpoints["endpoint-a"].URL)
	}

	// Verify models
	if len(cfg.Models) != 1 {
		t.Errorf("len(Models) = %d, want 1", len(cfg.Models))
	}
	if len(cfg.Models["claude-sonnet-4-20250514"].Backends) != 1 {
		t.Errorf("Backends count = %d, want 1", len(cfg.Models["claude-sonnet-4-20250514"].Backends))
	}

	// Verify health config
	if cfg.EndpointHealth.FailuresToDisable != 5 {
		t.Errorf("FailuresToDisable = %d, want 5", cfg.EndpointHealth.FailuresToDisable)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Load() expected error for missing file")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	content := `invalid: yaml: content: [`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString(content)
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Error("Load() expected error for invalid YAML")
	}
}

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "simple expansion",
			input:    `api_key: "${MY_KEY}"`,
			envVars:  map[string]string{"MY_KEY": "secret123"},
			expected: `api_key: "secret123"`,
		},
		{
			name:     "with default value",
			input:    `api_key: "${MY_KEY:-default-key}"`,
			envVars:  map[string]string{},
			expected: `api_key: "default-key"`,
		},
		{
			name:     "env overrides default",
			input:    `api_key: "${MY_KEY:-default-key}"`,
			envVars:  map[string]string{"MY_KEY": "actual-key"},
			expected: `api_key: "actual-key"`,
		},
		{
			name:     "no substitution",
			input:    `url: "https://api.anthropic.com"`,
			envVars:  map[string]string{},
			expected: `url: "https://api.anthropic.com"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Server: ServerConfig{Listen: ":8080"},
				Endpoints: map[string]EndpointConfig{
					"endpoint-a": {URL: "https://api.anthropic.com"},
				},
				Models: map[string]ModelConfig{
					"claude-sonnet": {Backends: []BackendConfig{{Endpoint: "endpoint-a"}}},
				},
			},
			wantErr: false,
		},
		{
			name: "missing listen",
			config: Config{
				Server: ServerConfig{Listen: ""},
				Endpoints: map[string]EndpointConfig{
					"endpoint-a": {URL: "https://api.anthropic.com"},
				},
				Models: map[string]ModelConfig{
					"claude-sonnet": {Backends: []BackendConfig{{Endpoint: "endpoint-a"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "missing endpoints",
			config: Config{
				Server:    ServerConfig{Listen: ":8080"},
				Endpoints: map[string]EndpointConfig{},
				Models: map[string]ModelConfig{
					"claude-sonnet": {Backends: []BackendConfig{{Endpoint: "endpoint-a"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid endpoint reference",
			config: Config{
				Server: ServerConfig{Listen: ":8080"},
				Endpoints: map[string]EndpointConfig{
					"endpoint-a": {URL: "https://api.anthropic.com"},
				},
				Models: map[string]ModelConfig{
					"claude-sonnet": {Backends: []BackendConfig{{Endpoint: "nonexistent"}}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
