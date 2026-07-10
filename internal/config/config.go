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
	ListenAddress        string         `yaml:"listen_address"`
	DataDir              string         `yaml:"data_dir"`
	PollIntervalSeconds  int            `yaml:"poll_interval_seconds"`
	SampleRetentionHours int            `yaml:"sample_retention_hours"`
	AllowedCIDRs         []string       `yaml:"allowed_cidrs"`
	RouterOS             RouterOSConfig `yaml:"routeros"`
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
		ListenAddress:        ":8080",
		DataDir:              "./data",
		PollIntervalSeconds:  5,
		SampleRetentionHours: 48,
	}

	if path != "" {
		payload, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(payload, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	overrideFromEnv(&cfg)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	absDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = absDataDir

	return cfg, nil
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
	if strings.TrimSpace(c.RouterOS.BaseURL) == "" {
		return errors.New("routeros.base_url is required")
	}
	if strings.TrimSpace(c.RouterOS.Username) == "" {
		return errors.New("routeros.username is required")
	}
	if strings.TrimSpace(c.RouterOS.Password) == "" {
		return errors.New("routeros.password is required")
	}
	if c.PollIntervalSeconds <= 0 {
		return errors.New("poll_interval_seconds must be positive")
	}
	if c.SampleRetentionHours <= 0 {
		return errors.New("sample_retention_hours must be positive")
	}
	return nil
}
