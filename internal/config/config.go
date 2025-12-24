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

	Sources []SourceConfig `yaml:"sources"`

	HealthCheck HealthCheckConfig `yaml:"health_check"`
	Ports       PortsConfig       `yaml:"ports"`
	Logging     LoggingConfig     `yaml:"logging"`
	Auth        AuthConfig        `yaml:"auth"`
	Admin       AdminConfig       `yaml:"admin"`
	Selection   SelectionConfig   `yaml:"selection"`

	Adapters AdaptersConfig `yaml:"adapters"`
}

type SourceConfig struct {
	// Type supports:
	// - raw_list: line-based lists (ip:port; socks5://ip:port also accepted)
	// - clash_yaml: Clash format YAML (URL or local file path)
	Type string `yaml:"type"`

	URL  string `yaml:"url"`
	Path string `yaml:"path"`
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

	Sticky StickyConfig `yaml:"sticky"`
}

type StickyConfig struct {
	Enabled bool `yaml:"enabled"`
	// HeaderOverride controls whether request headers can override sticky behavior.
	HeaderOverride *bool `yaml:"header_override"`
	// Failover controls how a request attempts another upstream on failure:
	// - soft: try the next HRW-ranked upstream
	// - hard: prefer staying on the top HRW-ranked upstream
	Failover string `yaml:"failover"`
}

type AdaptersConfig struct {
	Xray XrayConfig `yaml:"xray"`
}

type XrayConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BinaryPath string `yaml:"binary_path"`
	WorkDir    string `yaml:"work_dir"`

	SOCKSListenStrict  string `yaml:"socks_listen_strict"`
	SOCKSListenRelaxed string `yaml:"socks_listen_relaxed"`

	MetricsListenStrict  string `yaml:"metrics_listen_strict"`
	MetricsListenRelaxed string `yaml:"metrics_listen_relaxed"`

	// UserPassword is used as a shared password for all per-node SOCKS accounts
	// (username = nodeID). It should only be exposed on loopback interfaces.
	UserPassword string `yaml:"user_password"`

	Observatory ObservatoryConfig `yaml:"observatory"`

	MaxNodes            int `yaml:"max_nodes"`
	StartTimeoutSeconds int `yaml:"start_timeout_seconds"`

	// If true, updater will fall back to legacy SOCKS5 list mode when xray startup/metrics fail.
	FallbackToLegacyOnError *bool `yaml:"fallback_to_legacy_on_error"`
}

type ObservatoryConfig struct {
	// Mode supports:
	// - burst: use burstObservatory (recommended for large node counts)
	// - observatory: use observatory
	Mode string `yaml:"mode"`

	Destination  string `yaml:"destination"`
	Connectivity string `yaml:"connectivity"`

	IntervalSeconds int `yaml:"interval_seconds"`
	Sampling        int `yaml:"sampling"`
	TimeoutSeconds  int `yaml:"timeout_seconds"`
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
		cfg.Ports.SOCKS5Strict = ""
	}
	if cfg.Ports.SOCKS5Relaxed == "" {
		cfg.Ports.SOCKS5Relaxed = ":17283"
	}
	if cfg.Ports.HTTPStrict == "" {
		cfg.Ports.HTTPStrict = ""
	}
	if cfg.Ports.HTTPRelaxed == "" {
		cfg.Ports.HTTPRelaxed = ":17285"
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
	if cfg.Selection.Sticky.HeaderOverride == nil {
		b := true
		cfg.Selection.Sticky.HeaderOverride = &b
	}
	if cfg.Selection.Sticky.Failover == "" {
		cfg.Selection.Sticky.Failover = "soft"
	}

	if cfg.Adapters.Xray.WorkDir == "" {
		cfg.Adapters.Xray.WorkDir = ".easyproxypool/xray"
	}
	if cfg.Adapters.Xray.SOCKSListenStrict == "" {
		cfg.Adapters.Xray.SOCKSListenStrict = ""
	}
	if cfg.Adapters.Xray.SOCKSListenRelaxed == "" {
		cfg.Adapters.Xray.SOCKSListenRelaxed = "127.0.0.1:17383"
	}
	if cfg.Adapters.Xray.MetricsListenStrict == "" {
		cfg.Adapters.Xray.MetricsListenStrict = ""
	}
	if cfg.Adapters.Xray.MetricsListenRelaxed == "" {
		cfg.Adapters.Xray.MetricsListenRelaxed = "127.0.0.1:17387"
	}
	if cfg.Adapters.Xray.UserPassword == "" {
		cfg.Adapters.Xray.UserPassword = "easyproxypool"
	}
	if cfg.Adapters.Xray.Observatory.Mode == "" {
		cfg.Adapters.Xray.Observatory.Mode = "burst"
	}
	if cfg.Adapters.Xray.Observatory.Destination == "" {
		cfg.Adapters.Xray.Observatory.Destination = "https://www.gstatic.com/generate_204"
	}
	if cfg.Adapters.Xray.Observatory.IntervalSeconds <= 0 {
		cfg.Adapters.Xray.Observatory.IntervalSeconds = 30
	}
	if cfg.Adapters.Xray.Observatory.Sampling <= 0 {
		cfg.Adapters.Xray.Observatory.Sampling = 5
	}
	if cfg.Adapters.Xray.Observatory.TimeoutSeconds <= 0 {
		cfg.Adapters.Xray.Observatory.TimeoutSeconds = 5
	}
	if cfg.Adapters.Xray.MaxNodes <= 0 {
		cfg.Adapters.Xray.MaxNodes = 2000
	}
	if cfg.Adapters.Xray.StartTimeoutSeconds <= 0 {
		cfg.Adapters.Xray.StartTimeoutSeconds = 10
	}
	if cfg.Adapters.Xray.FallbackToLegacyOnError == nil {
		b := true
		cfg.Adapters.Xray.FallbackToLegacyOnError = &b
	}
}

func validate(cfg Config) error {
	if len(cfg.ProxyListURLs) == 0 && len(cfg.Sources) == 0 {
		return fmt.Errorf("proxy_list_urls or sources: at least one source is required")
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
	switch cfg.Selection.Sticky.Failover {
	case "soft", "hard":
	default:
		return fmt.Errorf("selection.sticky.failover: unsupported %q (use soft or hard)", cfg.Selection.Sticky.Failover)
	}

	if cfg.Adapters.Xray.Enabled {
		if cfg.Adapters.Xray.BinaryPath == "" {
			return fmt.Errorf("adapters.xray.binary_path: required when adapters.xray.enabled=true")
		}
		if cfg.Adapters.Xray.SOCKSListenRelaxed == "" {
			return fmt.Errorf("adapters.xray.socks_listen_relaxed: required when adapters.xray.enabled=true")
		}
		if cfg.Adapters.Xray.MetricsListenRelaxed == "" {
			return fmt.Errorf("adapters.xray.metrics_listen_relaxed: required when adapters.xray.enabled=true")
		}
		if cfg.Adapters.Xray.UserPassword == "" {
			return fmt.Errorf("adapters.xray.user_password: required when adapters.xray.enabled=true")
		}
		switch cfg.Adapters.Xray.Observatory.Mode {
		case "burst", "observatory":
		default:
			return fmt.Errorf("adapters.xray.observatory.mode: unsupported %q (use burst or observatory)", cfg.Adapters.Xray.Observatory.Mode)
		}
		if cfg.Adapters.Xray.Observatory.Destination == "" {
			return fmt.Errorf("adapters.xray.observatory.destination: required when adapters.xray.enabled=true")
		}
		if cfg.Adapters.Xray.MaxNodes <= 0 {
			return fmt.Errorf("adapters.xray.max_nodes: must be > 0")
		}
	}
	return nil
}
