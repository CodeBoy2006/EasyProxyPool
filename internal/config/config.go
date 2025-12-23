package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ProxyListURLs          []string `yaml:"proxy_list_urls"`
	HealthCheckConcurrency int      `yaml:"health_check_concurrency"`
	UpdateIntervalMinutes  int      `yaml:"update_interval_minutes"`

	HealthCheck HealthCheckConfig `yaml:"health_check"`
	Ports       PortsConfig       `yaml:"ports"`
	Logging     LoggingConfig     `yaml:"logging"`
	Auth        AuthConfig        `yaml:"auth"`
	Admin       AdminConfig       `yaml:"admin"`
	Selection   SelectionConfig   `yaml:"selection"`
}

type HealthCheckConfig struct {
	TotalTimeoutSeconds          int    `yaml:"total_timeout_seconds"`
	TLSHandshakeThresholdSeconds int    `yaml:"tls_handshake_threshold_seconds"`
	TargetAddress                string `yaml:"target_address"`
	TargetServerName             string `yaml:"target_server_name"`
}

type PortsConfig struct {
	SOCKS5Strict  string `yaml:"socks5_strict"`
	SOCKS5Relaxed string `yaml:"socks5_relaxed"`
	HTTPStrict    string `yaml:"http_strict"`
	HTTPRelaxed   string `yaml:"http_relaxed"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type AdminConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
}

type SelectionConfig struct {
	Strategy              string `yaml:"strategy"`
	Retries               int    `yaml:"retries"`
	FailureBackoffSeconds int    `yaml:"failure_backoff_seconds"`
	MaxBackoffSeconds     int    `yaml:"max_backoff_seconds"`
	RetryNonIdempotent    bool   `yaml:"retry_non_idempotent"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse yaml: %w", err)
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.HealthCheckConcurrency <= 0 {
		cfg.HealthCheckConcurrency = 200
	}
	if cfg.UpdateIntervalMinutes <= 0 {
		cfg.UpdateIntervalMinutes = 5
	}
	if cfg.HealthCheck.TotalTimeoutSeconds <= 0 {
		cfg.HealthCheck.TotalTimeoutSeconds = 8
	}
	if cfg.HealthCheck.TLSHandshakeThresholdSeconds <= 0 {
		cfg.HealthCheck.TLSHandshakeThresholdSeconds = 5
	}
	if cfg.HealthCheck.TargetAddress == "" {
		cfg.HealthCheck.TargetAddress = "www.google.com:443"
	}
	if cfg.HealthCheck.TargetServerName == "" {
		cfg.HealthCheck.TargetServerName = "www.google.com"
	}
	if cfg.Ports.SOCKS5Strict == "" {
		cfg.Ports.SOCKS5Strict = ":17283"
	}
	if cfg.Ports.SOCKS5Relaxed == "" {
		cfg.Ports.SOCKS5Relaxed = ":17284"
	}
	if cfg.Ports.HTTPStrict == "" {
		cfg.Ports.HTTPStrict = ":17285"
	}
	if cfg.Ports.HTTPRelaxed == "" {
		cfg.Ports.HTTPRelaxed = ":17286"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Admin.Addr == "" {
		cfg.Admin.Addr = ":17287"
	}
	if cfg.Selection.Strategy == "" {
		cfg.Selection.Strategy = "round_robin"
	}
	if cfg.Selection.Retries < 0 {
		cfg.Selection.Retries = 0
	}
	if cfg.Selection.FailureBackoffSeconds <= 0 {
		cfg.Selection.FailureBackoffSeconds = 30
	}
	if cfg.Selection.MaxBackoffSeconds <= 0 {
		cfg.Selection.MaxBackoffSeconds = 600
	}
}

func validate(cfg Config) error {
	if len(cfg.ProxyListURLs) == 0 {
		return fmt.Errorf("proxy_list_urls: at least one URL is required")
	}
	if cfg.HealthCheckConcurrency <= 0 {
		return fmt.Errorf("health_check_concurrency: must be > 0")
	}
	if cfg.UpdateIntervalMinutes <= 0 {
		return fmt.Errorf("update_interval_minutes: must be > 0")
	}
	switch cfg.Selection.Strategy {
	case "round_robin", "random":
	default:
		return fmt.Errorf("selection.strategy: unsupported %q (use round_robin or random)", cfg.Selection.Strategy)
	}
	return nil
}
