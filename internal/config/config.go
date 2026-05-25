package config

import (
	"fmt"
	"net/url"
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

// EndpointConfig holds endpoint configuration
type EndpointConfig struct {
	URL            string        `yaml:"url"`
	ModelsEndpoint string        `yaml:"models_endpoint"` // Optional: custom URL for model discovery
	APIKey         string        `yaml:"api_key"` // Supports ${ENV_VAR}
	Timeout        time.Duration `yaml:"timeout"`
	Offline        bool          `yaml:"offline"` // Permanently disable endpoint
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

	// Validate models_endpoint URLs if provided
	for name, ep := range c.Endpoints {
		if ep.ModelsEndpoint != "" {
			if _, err := url.Parse(ep.ModelsEndpoint); err != nil {
				return fmt.Errorf("endpoint %s models_endpoint must be a valid URL: %w", name, err)
			}
		}
	}

	return nil
}
