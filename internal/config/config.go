package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path                        string         `yaml:"-"`
	ListenAddress               string         `yaml:"listen_address"`
	DataDir                     string         `yaml:"data_dir"`
	PollIntervalSeconds         int            `yaml:"poll_interval_seconds"`
	RealtimePollIntervalSeconds int            `yaml:"realtime_poll_interval_seconds"`
	TerminalPollIntervalSeconds int            `yaml:"terminal_poll_interval_seconds"`
	SampleRetentionHours        int            `yaml:"sample_retention_hours"`
	AllowedCIDRs                []string       `yaml:"allowed_cidrs"`
	RouterOS                    RouterOSConfig `yaml:"routeros"`
}

type RouterOSConfig struct {
	BaseURL           string   `yaml:"base_url"`
	Username          string   `yaml:"username"`
	Password          string   `yaml:"password"`
	TrafficInterfaces []string `yaml:"traffic_interfaces"`
	TerminalCIDRs     []string `yaml:"terminal_cidrs"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		ListenAddress:               ":8080",
		DataDir:                     "./data",
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 3,
		SampleRetentionHours:        48,
		RouterOS: RouterOSConfig{
			BaseURL: "http://10.0.0.1",
		},
	}

	if path != "" {
		payload, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
		} else if err := yaml.Unmarshal(payload, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	overrideFromEnv(&cfg)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	cfg.Path = path
	return finalize(cfg)
}

func finalize(cfg Config) (Config, error) {
	absDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = absDataDir

	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.Path = ""
	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func overrideFromEnv(cfg *Config) {
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_LISTEN_ADDRESS")); value != "" {
		cfg.ListenAddress = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_DATA_DIR")); value != "" {
		cfg.DataDir = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_ROUTEROS_BASE_URL")); value != "" {
		cfg.RouterOS.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_ROUTEROS_USERNAME")); value != "" {
		cfg.RouterOS.Username = value
	}
	if value := os.Getenv("ROSBOARD_ROUTEROS_PASSWORD"); value != "" {
		cfg.RouterOS.Password = value
	}
}

func (c Config) validate() error {
	if c.PollIntervalSeconds <= 0 {
		return errors.New("poll_interval_seconds must be positive")
	}
	if c.RealtimePollIntervalSeconds <= 0 {
		return errors.New("realtime_poll_interval_seconds must be positive")
	}
	if c.TerminalPollIntervalSeconds <= 0 {
		return errors.New("terminal_poll_interval_seconds must be positive")
	}
	if c.SampleRetentionHours <= 0 {
		return errors.New("sample_retention_hours must be positive")
	}
	return nil
}

func (c Config) RouterOSConfigured() bool {
	return c.RouterOS.Configured()
}

func (c RouterOSConfig) Configured() bool {
	return configuredValue(c.BaseURL) && configuredValue(c.Username) && configuredValue(c.Password)
}

func configuredValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "replace-me"
}
