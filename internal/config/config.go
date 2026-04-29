package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure
type Config struct {
	Server         ServerConfig              `yaml:"server"`
	Logging        LoggingConfig             `yaml:"logging"`
	Metrics        MetricsConfig             `yaml:"metrics"`
	Health         HealthConfig              `yaml:"health"`
	Routing        RoutingConfig             `yaml:"routing"`
	Models         map[string]ModelConfig    `yaml:"models"`
	Endpoints      map[string]EndpointConfig `yaml:"endpoints"`
	EndpointHealth EndpointHealthConfig      `yaml:"endpoint_health"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Listen       string        `yaml:"listen"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MetricsConfig holds Prometheus metrics settings
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// HealthConfig holds health endpoint settings
type HealthConfig struct {
	Path string `yaml:"path"`
}

// RoutingConfig holds routing strategy settings
type RoutingConfig struct {
	DefaultStrategy string `yaml:"default_strategy"`
}

// ModelConfig holds model mapping configuration
type ModelConfig struct {
	Strategy string         `yaml:"strategy"` // Optional override
	Backends []BackendConfig `yaml:"backends"`
}

// BackendConfig holds backend endpoint configuration for a model
type BackendConfig struct {
	Endpoint string `yaml:"endpoint"` // References endpoint name
	Model    string `yaml:"model"`    // Backend model name
	Weight   int    `yaml:"weight"`   // For weighted strategies
}

// EndpointConfig holds endpoint configuration
type EndpointConfig struct {
	URL     string        `yaml:"url"`
	APIKey  string        `yaml:"api_key"` // Supports ${ENV_VAR}
	Timeout time.Duration `yaml:"timeout"`
}

// EndpointHealthConfig holds health management settings
type EndpointHealthConfig struct {
	FailuresToDisable     int           `yaml:"failures_to_disable"`
	RecoveryProbeInterval time.Duration `yaml:"recovery_probe_interval"`
	SuccessesToEnable     int           `yaml:"successes_to_enable"`
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Expand environment variables before parsing
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// expandEnvVars replaces ${VAR} and ${VAR:-default} with environment values
func expandEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([^}:]+)(:-([^}]*))?\}`)

	return re.ReplaceAllStringFunc(s, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		varName := submatches[1]
		defaultVal := ""
		if len(submatches) > 3 {
			defaultVal = submatches[3]
		}

		val := os.Getenv(varName)
		if val == "" {
			val = defaultVal
		}
		return val
	})
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}

	if len(c.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}

	if len(c.Models) == 0 {
		return fmt.Errorf("at least one model mapping is required")
	}

	// Validate that all model backends reference valid endpoints
	for modelName, modelCfg := range c.Models {
		for _, backend := range modelCfg.Backends {
			if _, exists := c.Endpoints[backend.Endpoint]; !exists {
				return fmt.Errorf("model %q references unknown endpoint %q", modelName, backend.Endpoint)
			}
		}
	}

	return nil
}
