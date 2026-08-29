package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/recognition"
)

type FeatureLibraryStatus struct {
	Enabled              bool      `json:"enabled"`
	SourceURL            string    `json:"sourceUrl"`
	RefreshIntervalHours int       `json:"refreshIntervalHours"`
	MatchWindowMinutes   int       `json:"matchWindowMinutes"`
	RuleCount            int       `json:"ruleCount"`
	LastAttempt          time.Time `json:"lastAttempt,omitempty"`
	LastSuccess          time.Time `json:"lastSuccess,omitempty"`
	LastError            string    `json:"lastError,omitempty"`
}

type FeatureLibrarySynchronizer struct {
	client    *http.Client
	config    config.FeatureLibraryConfig
	cachePath string
	logger    *log.Logger
	interval  time.Duration

	mu      sync.RWMutex
	library *recognition.Library
	status  FeatureLibraryStatus
}

// NewFeatureLibrarySynchronizer builds one library synchronizer per device.
// Each device keeps its own cache file so independent source URLs, refresh
// cycles, and rule sets never bleed across routers.
func NewFeatureLibrarySynchronizer(cfg config.FeatureLibraryConfig, dataDir string, logger *log.Logger, deviceID string) (*FeatureLibrarySynchronizer, error) {
	if !cfg.Configured() {
		return nil, nil
	}
	parsed, err := url.Parse(strings.TrimSpace(cfg.SourceURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("feature library source URL must use http or https")
	}
	if cfg.RefreshIntervalHours <= 0 || cfg.MatchWindowMinutes <= 0 {
		return nil, fmt.Errorf("feature library intervals must be positive")
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "."
	}
	synchronizer := &FeatureLibrarySynchronizer{
		client:    &http.Client{Timeout: 30 * time.Second},
		config:    cfg,
		cachePath: featureLibraryCachePath(dataDir, deviceID),
		logger:    logger,
		interval:  time.Duration(cfg.RefreshIntervalHours) * time.Hour,
		status: FeatureLibraryStatus{
			Enabled:              true,
			SourceURL:            cfg.SourceURL,
			RefreshIntervalHours: cfg.RefreshIntervalHours,
			MatchWindowMinutes:   cfg.MatchWindowMinutes,
		},
	}
	if payload, err := os.ReadFile(synchronizer.cachePath); err == nil {
		if library, parseErr := recognition.Parse(payload); parseErr == nil {
			synchronizer.library = library
			synchronizer.status.RuleCount = library.RuleCount()
		} else {
			synchronizer.status.LastError = parseErr.Error()
		}
	}
	return synchronizer, nil
}

// featureLibraryCachePath derives the per-device cache file name. The default
// device keeps the legacy shared file name so existing caches remain valid.
// Sanitized device IDs get a short hash suffix so two different IDs can never
// collide on the same cache file (same strategy as device database names).
func featureLibraryCachePath(dataDir, deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || deviceID == "default" {
		return filepath.Join(dataDir, "feature-library.yml")
	}
	var builder strings.Builder
	sanitized := true
	for _, character := range deviceID {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
			sanitized = false
		}
	}
	name := builder.String()
	if !sanitized {
		sum := sha256.Sum256([]byte(deviceID))
		name = fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:4]))
	}
	return filepath.Join(dataDir, "feature-library-"+name+".yml")
}

func (s *FeatureLibrarySynchronizer) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx.Err() == nil {
		_ = s.Refresh(ctx)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Refresh(ctx)
		}
	}
}

func (s *FeatureLibrarySynchronizer) Refresh(ctx context.Context) error {
	if s == nil {
		return nil
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.status.LastAttempt = now
	s.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.SourceURL, nil)
	if err != nil {
		return s.fail(err)
	}
	request.Header.Set("Accept", "text/yaml, application/yaml, text/plain")
	response, err := s.client.Do(request)
	if err != nil {
		return s.fail(fmt.Errorf("request feature library: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return s.fail(fmt.Errorf("feature library returned %s", response.Status))
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return s.fail(fmt.Errorf("read feature library: %w", err))
	}
	library, err := recognition.Parse(payload)
	if err != nil {
		return s.fail(err)
	}
	if err := writeFeatureLibraryCache(s.cachePath, payload); err != nil {
		return s.fail(err)
	}
	s.mu.Lock()
	s.library = library
	s.status.RuleCount = library.RuleCount()
	s.status.LastSuccess = now
	s.status.LastError = ""
	s.mu.Unlock()
	return nil
}

func (s *FeatureLibrarySynchronizer) Lookup(domain string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	library := s.library
	s.mu.RUnlock()
	return library.Lookup(domain)
}

func (s *FeatureLibrarySynchronizer) Status() FeatureLibraryStatus {
	if s == nil {
		return FeatureLibraryStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *FeatureLibrarySynchronizer) fail(err error) error {
	s.mu.Lock()
	s.status.LastError = err.Error()
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Printf("feature library refresh failed: %v", err)
	}
	return err
}

func writeFeatureLibraryCache(path string, payload []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create feature library directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".feature-library-*")
	if err != nil {
		return fmt.Errorf("create feature library cache: %w", err)
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
		return fmt.Errorf("set feature library cache permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write feature library cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync feature library cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close feature library cache: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace feature library cache: %w", err)
	}
	removeTemporary = false
	return nil
}
