package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path                        string               `yaml:"-"`
	MigrationPending            bool                 `yaml:"-"`
	RecognitionDefaultsMigrated bool                 `yaml:"recognition_defaults_migrated,omitempty"`
	ListenAddress               string               `yaml:"listen_address"`
	DataDir                     string               `yaml:"data_dir"`
	PollIntervalSeconds         int                  `yaml:"poll_interval_seconds"`
	RealtimePollIntervalSeconds int                  `yaml:"realtime_poll_interval_seconds"`
	TerminalPollIntervalSeconds int                  `yaml:"terminal_poll_interval_seconds"`
	SampleRetentionHours        int                  `yaml:"sample_retention_hours"`
	AllowedCIDRs                []string             `yaml:"allowed_cidrs"`
	MosDNS                      MosDNSConfig         `yaml:"mosdns,omitempty"`
	FeatureLibrary              FeatureLibraryConfig `yaml:"feature_library,omitempty"`
	RouterOS                    RouterOSConfig       `yaml:"routeros,omitempty"`
	Devices                     []DeviceConfig       `yaml:"devices,omitempty"`
}

type MosDNSConfig struct {
	Enabled             bool   `yaml:"enabled" json:"enabled"`
	BaseURL             string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	SyncIntervalMinutes int    `yaml:"sync_interval_minutes,omitempty" json:"sync_interval_minutes,omitempty"`
}

type FeatureLibraryConfig struct {
	Enabled              bool   `yaml:"enabled" json:"enabled"`
	SourceURL            string `yaml:"source_url,omitempty" json:"source_url,omitempty"`
	RefreshIntervalHours int    `yaml:"refresh_interval_hours,omitempty" json:"refresh_interval_hours,omitempty"`
	MatchWindowMinutes   int    `yaml:"match_window_minutes,omitempty" json:"match_window_minutes,omitempty"`
}

const DefaultDeviceID = "default"

const defaultFeatureLibrarySourceURL = "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat_plain.yml"

type DeviceConfig struct {
	ID             string                  `yaml:"id"`
	Name           string                  `yaml:"name"`
	Enabled        bool                    `yaml:"enabled"`
	Archived       bool                    `yaml:"archived,omitempty"`
	ManagedAccount *ManagedRouterOSAccount `yaml:"managed_account,omitempty"`
	RouterOS       RouterOSConfig          `yaml:"routeros"`
}

type ManagedRouterOSAccount struct {
	Username  string `yaml:"username"`
	GroupName string `yaml:"group_name"`
}

type RouterOSConfig struct {
	BaseURL  string `yaml:"base_url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// TrafficInterfaces is the legacy manual traffic-selection setting.
	TrafficInterfaces []string            `yaml:"traffic_interfaces,omitempty"`
	TrafficScope      TrafficScopeConfig  `yaml:"traffic_scope,omitempty"`
	TerminalCIDRs     []string            `yaml:"terminal_cidrs,omitempty"`
	TerminalScope     TerminalScopeConfig `yaml:"terminal_scope,omitempty"`
}

type TrafficScopeConfig struct {
	Mode              string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	IncludeInterfaces []string `yaml:"include_interfaces,omitempty" json:"include_interfaces,omitempty"`
	ExcludeInterfaces []string `yaml:"exclude_interfaces,omitempty" json:"exclude_interfaces,omitempty"`
}

type TerminalScopeConfig struct {
	Mode              string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	IncludeInterfaces []string `yaml:"include_interfaces,omitempty" json:"include_interfaces,omitempty"`
	ExcludeInterfaces []string `yaml:"exclude_interfaces,omitempty" json:"exclude_interfaces,omitempty"`
	IncludeCIDRs      []string `yaml:"include_cidrs,omitempty" json:"include_cidrs,omitempty"`
	ExcludeCIDRs      []string `yaml:"exclude_cidrs,omitempty" json:"exclude_cidrs,omitempty"`
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = "config.yaml"
	}
	cfg := Config{
		ListenAddress:               ":8080",
		DataDir:                     "./data",
		PollIntervalSeconds:         10,
		RealtimePollIntervalSeconds: 1,
		TerminalPollIntervalSeconds: 5,
		SampleRetentionHours:        48,
		MosDNS: MosDNSConfig{
			Enabled:             false,
			BaseURL:             "",
			SyncIntervalMinutes: 30,
		},
		FeatureLibrary: FeatureLibraryConfig{
			Enabled:              false,
			SourceURL:            "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat_plain.yml",
			RefreshIntervalHours: 168,
			MatchWindowMinutes:   30,
		},
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

	cfg.migrateRecognitionDefaults()
	overrideFromEnv(&cfg)
	cfg.normalizeMosDNS()
	cfg.normalizeFeatureLibrary()
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
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".rosboard-config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	removeTemporary = false
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
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_MOSDNS_BASE_URL")); value != "" {
		cfg.MosDNS.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_MOSDNS_SYNC_INTERVAL_MINUTES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.MosDNS.SyncIntervalMinutes = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_FEATURE_LIBRARY_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.FeatureLibrary.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_FEATURE_LIBRARY_SOURCE_URL")); value != "" {
		cfg.FeatureLibrary.SourceURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_FEATURE_LIBRARY_REFRESH_INTERVAL_HOURS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.FeatureLibrary.RefreshIntervalHours = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("ROSBOARD_FEATURE_LIBRARY_MATCH_WINDOW_MINUTES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.FeatureLibrary.MatchWindowMinutes = parsed
		}
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
	if c.MosDNS.Enabled && strings.TrimSpace(c.MosDNS.BaseURL) == "" {
		return errors.New("mosdns.base_url is required when enabled")
	}
	if c.MosDNS.Configured() && c.MosDNS.SyncIntervalMinutes <= 0 {
		return errors.New("mosdns.sync_interval_minutes must be positive")
	}
	if c.FeatureLibrary.Configured() {
		if c.FeatureLibrary.RefreshIntervalHours <= 0 {
			return errors.New("feature_library.refresh_interval_hours must be positive")
		}
		if c.FeatureLibrary.MatchWindowMinutes <= 0 {
			return errors.New("feature_library.match_window_minutes must be positive")
		}
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
		if err := device.RouterOS.TerminalScope.validate(); err != nil {
			return fmt.Errorf("devices[%d].routeros.terminal_scope: %w", index, err)
		}
		if err := device.RouterOS.TrafficScope.validate(); err != nil {
			return fmt.Errorf("devices[%d].routeros.traffic_scope: %w", index, err)
		}
	}
	if len(c.Devices) == 0 {
		if err := c.RouterOS.TerminalScope.validate(); err != nil {
			return fmt.Errorf("routeros.terminal_scope: %w", err)
		}
		if err := c.RouterOS.TrafficScope.validate(); err != nil {
			return fmt.Errorf("routeros.traffic_scope: %w", err)
		}
	}
	return nil
}

func (c *Config) normalizeMosDNS() {
	c.MosDNS.BaseURL = NormalizeMosDNSBaseURL(c.MosDNS.BaseURL)
	if c.MosDNS.SyncIntervalMinutes == 0 {
		c.MosDNS.SyncIntervalMinutes = 30
	}
}

// NormalizeMosDNSBaseURL keeps the config/client contract URL-shaped while
// allowing the settings UI to accept a plain address such as 10.0.0.3.
func NormalizeMosDNSBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return value
	}
	return "http://" + value
}

func (c MosDNSConfig) Configured() bool {
	return c.Enabled && strings.TrimSpace(c.BaseURL) != ""
}

func (c *Config) normalizeFeatureLibrary() {
	if strings.TrimSpace(c.FeatureLibrary.SourceURL) == "" {
		c.FeatureLibrary.SourceURL = defaultFeatureLibrarySourceURL
	}
	if c.FeatureLibrary.RefreshIntervalHours == 0 {
		c.FeatureLibrary.RefreshIntervalHours = 168
	}
	if c.FeatureLibrary.MatchWindowMinutes == 0 {
		c.FeatureLibrary.MatchWindowMinutes = 30
	}
}

func (c *Config) migrateRecognitionDefaults() {
	if c.RecognitionDefaultsMigrated {
		return
	}

	legacyMosDNSAddress := NormalizeMosDNSBaseURL(c.MosDNS.BaseURL)
	if c.MosDNS.Enabled && (strings.TrimSpace(c.MosDNS.BaseURL) == "" || (legacyMosDNSAddress == "http://10.0.0.3" && c.MosDNS.SyncIntervalMinutes == 30)) {
		c.MosDNS.Enabled = false
		c.MosDNS.BaseURL = ""
		c.MigrationPending = true
	}
	if c.FeatureLibrary.Enabled && (strings.TrimSpace(c.FeatureLibrary.SourceURL) == "" || (c.FeatureLibrary.SourceURL == defaultFeatureLibrarySourceURL && c.FeatureLibrary.RefreshIntervalHours == 168 && c.FeatureLibrary.MatchWindowMinutes == 30)) {
		c.FeatureLibrary.Enabled = false
		c.MigrationPending = true
	}
	c.RecognitionDefaultsMigrated = true
}

func (c FeatureLibraryConfig) Configured() bool {
	return c.Enabled && strings.TrimSpace(c.SourceURL) != ""
}

func (scope TrafficScopeConfig) validate() error {
	included := make(map[string]struct{}, len(scope.IncludeInterfaces))
	for _, name := range scope.IncludeInterfaces {
		name = strings.TrimSpace(name)
		if name != "" {
			included[strings.ToLower(name)] = struct{}{}
		}
	}
	for _, name := range scope.ExcludeInterfaces {
		name = strings.TrimSpace(name)
		if name != "" {
			if _, exists := included[strings.ToLower(name)]; exists {
				return fmt.Errorf("interface %q is both included and excluded", name)
			}
		}
	}
	if mode := strings.TrimSpace(scope.Mode); mode != "" && !strings.EqualFold(mode, "auto") {
		return fmt.Errorf("mode must be auto")
	}
	return nil
}

func (scope TerminalScopeConfig) validate() error {
	included := make(map[string]struct{}, len(scope.IncludeInterfaces))
	for _, name := range scope.IncludeInterfaces {
		name = strings.TrimSpace(name)
		if name != "" {
			included[strings.ToLower(name)] = struct{}{}
		}
	}
	for _, name := range scope.ExcludeInterfaces {
		name = strings.TrimSpace(name)
		if name != "" {
			if _, exists := included[strings.ToLower(name)]; exists {
				return fmt.Errorf("interface %q is both included and excluded", name)
			}
		}
	}
	if mode := strings.TrimSpace(scope.Mode); mode != "" && !strings.EqualFold(mode, "auto") {
		return fmt.Errorf("mode must be auto")
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
