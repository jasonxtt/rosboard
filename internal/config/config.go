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
	RouterOS                    RouterOSConfig `yaml:"routeros,omitempty"`
	Devices                     []DeviceConfig `yaml:"devices,omitempty"`
}

const DefaultDeviceID = "default"

type DeviceConfig struct {
	ID       string         `yaml:"id"`
	Name     string         `yaml:"name"`
	Enabled  bool           `yaml:"enabled"`
	Archived bool           `yaml:"archived,omitempty"`
	RouterOS RouterOSConfig `yaml:"routeros"`
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
	cfg.normalizeDevices()

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
	if len(cfg.Devices) > 0 {
		cfg.RouterOS = RouterOSConfig{}
	}
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
	target := &cfg.RouterOS
	if len(cfg.Devices) > 0 {
		target = &cfg.Devices[0].RouterOS
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_LISTEN_ADDRESS")); value != "" {
		cfg.ListenAddress = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_DATA_DIR")); value != "" {
		cfg.DataDir = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_ROUTEROS_BASE_URL")); value != "" {
		target.BaseURL = value
		cfg.RouterOS.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_ROUTEROS_USERNAME")); value != "" {
		target.Username = value
		cfg.RouterOS.Username = value
	}
	if value := os.Getenv("ROSBOARD_ROUTEROS_PASSWORD"); value != "" {
		target.Password = value
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
	seen := make(map[string]struct{}, len(c.Devices))
	for index, device := range c.Devices {
		if strings.TrimSpace(device.ID) == "" {
			return fmt.Errorf("devices[%d].id is required", index)
		}
		if _, exists := seen[device.ID]; exists {
			return fmt.Errorf("devices[%d].id %q is duplicated", index, device.ID)
		}
		seen[device.ID] = struct{}{}
		if strings.TrimSpace(device.Name) == "" {
			return fmt.Errorf("devices[%d].name is required", index)
		}
	}
	return nil
}

func (c Config) RouterOSConfigured() bool {
	if len(c.Devices) == 0 {
		return c.RouterOS.Configured()
	}
	return len(c.ActiveDevices()) > 0
}

func (c Config) ActiveDevices() []DeviceConfig {
	devices := make([]DeviceConfig, 0, len(c.Devices))
	for _, device := range c.Devices {
		if device.Enabled && !device.Archived && device.RouterOS.Configured() {
			devices = append(devices, device)
		}
	}
	return devices
}

func (c Config) Device(id string) (DeviceConfig, bool) {
	for _, device := range c.Devices {
		if device.ID == id {
			return device, true
		}
	}
	return DeviceConfig{}, false
}

func (c *Config) normalizeDevices() {
	if len(c.Devices) == 0 {
		if c.RouterOS.Configured() {
			c.Devices = []DeviceConfig{{
				ID:       DefaultDeviceID,
				Name:     "RouterOS",
				Enabled:  true,
				RouterOS: c.RouterOS,
			}}
		}
		return
	}
	for index := range c.Devices {
		c.Devices[index].ID = strings.TrimSpace(c.Devices[index].ID)
		c.Devices[index].Name = strings.TrimSpace(c.Devices[index].Name)
	}
	for _, device := range c.Devices {
		if device.Enabled && !device.Archived {
			c.RouterOS = device.RouterOS
			return
		}
	}
	c.RouterOS = c.Devices[0].RouterOS
}

func (c RouterOSConfig) Configured() bool {
	return configuredValue(c.BaseURL) && configuredValue(c.Username) && configuredValue(c.Password)
}

func configuredValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "replace-me"
}
