package applicationcatalog

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultRefreshInterval = 168 * time.Hour
	maxCatalogBytes        = 16 << 20
	loadTimeout            = 30 * time.Second
)

// Application is the intentionally small application contract used by the
// first catalog slice. DomainSignatures contains only signatures that can be
// independently interpreted as domain matches.
type Application struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Category         string   `json:"category,omitempty"`
	DomainSignatures []string `json:"domainSignatures"`
}

type CatalogStatus struct {
	Source           string     `json:"source"`
	Version          string     `json:"version,omitempty"`
	LoadedAt         *time.Time `json:"loadedAt,omitempty"`
	LastSuccess      *time.Time `json:"lastSuccess,omitempty"`
	ApplicationCount int        `json:"applicationCount"`
	DomainCount      int        `json:"domainCount"`
	LastError        string     `json:"lastError,omitempty"`
}

// DomainMatch reports a successful or ambiguous domain lookup. A zero result
// means that the catalog has no matching domain.
type DomainMatch struct {
	Application   Application `json:"application,omitempty"`
	MatchedDomain string      `json:"matchedDomain,omitempty"`
	Ambiguous     bool        `json:"ambiguous,omitempty"`
}

type parsedCatalog struct {
	Version      string
	Applications []Application
}

type catalogSnapshot struct {
	applications map[string]Application
	domains      map[string]map[string]struct{}
}

type Catalog struct {
	source   string
	interval time.Duration

	mu      sync.RWMutex
	current *catalogSnapshot
	status  CatalogStatus
}

func New(source string, refreshInterval time.Duration) *Catalog {
	if refreshInterval <= 0 {
		refreshInterval = defaultRefreshInterval
	}
	source = strings.TrimSpace(source)
	return &Catalog{
		source:   source,
		interval: refreshInterval,
		status:   CatalogStatus{Source: source},
	}
}

// Start performs an initial refresh and then refreshes the configured source.
// Refresh errors are retained in Status; a failed refresh never replaces the
// current last-good snapshot.
func (c *Catalog) Start(ctx context.Context) {
	if c == nil || c.source == "" {
		return
	}
	_ = c.Refresh(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Refresh(ctx)
		}
	}
}

func (c *Catalog) Refresh(ctx context.Context) error {
	if c == nil {
		return errors.New("application catalog is nil")
	}
	if c.source == "" {
		err := errors.New("application catalog source is not configured")
		c.setError(err)
		return err
	}
	payload, err := readSource(ctx, c.source)
	if err != nil {
		c.setError(err)
		return err
	}
	parsed, err := parsePayload(payload)
	if err != nil {
		c.setError(err)
		return err
	}
	snapshot := makeSnapshot(parsed.Applications)
	now := time.Now().UTC()
	c.mu.Lock()
	c.current = snapshot
	c.status.Version = parsed.Version
	c.status.LoadedAt = timePtr(now)
	c.status.LastSuccess = timePtr(now)
	c.status.ApplicationCount = len(parsed.Applications)
	c.status.DomainCount = len(snapshot.domains)
	c.status.LastError = ""
	c.mu.Unlock()
	return nil
}

func (c *Catalog) Status() CatalogStatus {
	if c == nil {
		return CatalogStatus{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Catalog) Get(applicationID string) (Application, bool) {
	if c == nil {
		return Application{}, false
	}
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return Application{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return Application{}, false
	}
	application, ok := c.current.applications[applicationID]
	if !ok {
		return Application{}, false
	}
	return cloneApplication(application), true
}

func (c *Catalog) LookupDomain(domain string) DomainMatch {
	if c == nil {
		return DomainMatch{}
	}
	domain, ok := normalizeDomain(domain)
	if !ok {
		return DomainMatch{}
	}
	c.mu.RLock()
	snapshot := c.current
	if snapshot == nil {
		c.mu.RUnlock()
		return DomainMatch{}
	}
	for candidate := domain; candidate != ""; candidate = parentDomain(candidate) {
		ids := snapshot.domains[candidate]
		if len(ids) == 0 {
			continue
		}
		if len(ids) != 1 {
			c.mu.RUnlock()
			return DomainMatch{MatchedDomain: candidate, Ambiguous: true}
		}
		for id := range ids {
			application := cloneApplication(snapshot.applications[id])
			c.mu.RUnlock()
			return DomainMatch{Application: application, MatchedDomain: candidate}
		}
	}
	c.mu.RUnlock()
	return DomainMatch{}
}

func parsePayload(payload []byte) (parsedCatalog, error) {
	featureConfig, err := extractFeatureConfig(payload)
	if err != nil {
		return parsedCatalog{}, err
	}
	return parseFeatureConfig(featureConfig)
}

func parseFeatureConfig(payload []byte) (parsedCatalog, error) {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 1024), maxCatalogBytes)
	version := ""
	applications := make(map[string]Application)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			var err error
			switch {
			case strings.HasPrefix(line, "#version"):
				version, err = parseVersion(line)
			case strings.HasPrefix(line, "#format"):
				err = validateFormat(line)
			}
			if err != nil {
				return parsedCatalog{}, fmt.Errorf("line %d: %w", lineNumber, err)
			}
			continue
		}
		application, err := parseApplicationLine(line)
		if err != nil {
			return parsedCatalog{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if len(application.DomainSignatures) == 0 {
			continue
		}
		if previous, exists := applications[application.ID]; exists {
			if previous.Name != application.Name {
				return parsedCatalog{}, fmt.Errorf("application %s has conflicting names", application.ID)
			}
			previous.DomainSignatures = mergeDomains(previous.DomainSignatures, application.DomainSignatures)
			applications[application.ID] = previous
			continue
		}
		applications[application.ID] = application
	}
	if err := scanner.Err(); err != nil {
		return parsedCatalog{}, fmt.Errorf("read feature.cfg: %w", err)
	}
	if len(applications) == 0 {
		return parsedCatalog{}, errors.New("feature.cfg contains no supported domain signatures")
	}
	result := parsedCatalog{Version: version, Applications: make([]Application, 0, len(applications))}
	for _, application := range applications {
		application.DomainSignatures = mergeDomains(nil, application.DomainSignatures)
		result.Applications = append(result.Applications, application)
	}
	sort.Slice(result.Applications, func(i, j int) bool { return result.Applications[i].ID < result.Applications[j].ID })
	return result, nil
}

func parseVersion(line string) (string, error) {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[1] == "" {
		return "", errors.New("invalid #version header")
	}
	return fields[1], nil
}

func validateFormat(line string) error {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[1] != "v3.0" {
		return fmt.Errorf("unsupported OAF format %q", strings.TrimSpace(strings.TrimPrefix(line, "#format")))
	}
	return nil
}

func parseApplicationLine(line string) (Application, error) {
	open := strings.IndexByte(line, '[')
	close := strings.LastIndexByte(line, ']')
	if open <= 0 || close <= open {
		return Application{}, errors.New("invalid application signature")
	}
	if trailing := strings.TrimSpace(line[close+1:]); trailing != "" && !strings.HasPrefix(trailing, "#") {
		return Application{}, errors.New("unexpected text after application signature")
	}
	prefix := strings.TrimSpace(line[:open])
	colon := strings.LastIndexByte(prefix, ':')
	if colon <= 0 {
		return Application{}, errors.New("application name is missing")
	}
	head := strings.TrimSpace(prefix[:colon])
	fields := strings.Fields(head)
	if len(fields) < 2 {
		return Application{}, errors.New("application id or name is missing")
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return Application{}, fmt.Errorf("invalid OAF application id %q", fields[0])
	}
	name := strings.TrimSpace(strings.TrimPrefix(head, fields[0]))
	if name == "" {
		return Application{}, errors.New("application name is missing")
	}
	application := Application{ID: "oaf:" + strconv.FormatUint(value, 10), Name: name}
	for _, rawSignature := range strings.Split(line[open+1:close], ",") {
		if domain, ok := safeDomainSignature(rawSignature); ok {
			application.DomainSignatures = append(application.DomainSignatures, domain)
		}
	}
	application.DomainSignatures = mergeDomains(nil, application.DomainSignatures)
	return application, nil
}

func safeDomainSignature(raw string) (string, bool) {
	fields := strings.Split(strings.TrimSpace(raw), ";")
	if len(fields) < 4 {
		return "", false
	}
	protocol := strings.ToLower(strings.TrimSpace(fields[0]))
	if protocol != "" && protocol != "tcp" && protocol != "udp" {
		return "", false
	}
	if strings.TrimSpace(fields[1]) != "" || strings.TrimSpace(fields[2]) != "" {
		return "", false
	}
	for _, field := range fields[4:] {
		if strings.TrimSpace(field) != "" {
			return "", false
		}
	}
	host := strings.TrimSpace(fields[3])
	if strings.ContainsAny(host, " \t\r\n") {
		return "", false
	}
	return normalizeDomain(host)
}

func normalizeDomain(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil || strings.ContainsAny(value, "/\\:*?#[]") {
		return "", false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", false
		}
		for _, character := range label {
			if character != '-' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
				return "", false
			}
		}
	}
	return value, true
}

func parentDomain(domain string) string {
	index := strings.IndexByte(domain, '.')
	if index < 0 {
		return ""
	}
	return domain[index+1:]
}

func mergeDomains(existing, additions []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, domain := range existing {
		seen[domain] = struct{}{}
	}
	for _, domain := range additions {
		seen[domain] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for domain := range seen {
		result = append(result, domain)
	}
	sort.Strings(result)
	return result
}

func makeSnapshot(applications []Application) *catalogSnapshot {
	snapshot := &catalogSnapshot{
		applications: make(map[string]Application, len(applications)),
		domains:      make(map[string]map[string]struct{}),
	}
	for _, application := range applications {
		application = cloneApplication(application)
		snapshot.applications[application.ID] = application
		for _, domain := range application.DomainSignatures {
			ids := snapshot.domains[domain]
			if ids == nil {
				ids = make(map[string]struct{})
				snapshot.domains[domain] = ids
			}
			ids[application.ID] = struct{}{}
		}
	}
	return snapshot
}

func cloneApplication(application Application) Application {
	application.DomainSignatures = append([]string(nil), application.DomainSignatures...)
	return application
}

func readSource(ctx context.Context, source string) ([]byte, error) {
	lower := strings.ToLower(source)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		requestContext, cancel := context.WithTimeout(ctx, loadTimeout)
		defer cancel()
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, source, nil)
		if err != nil {
			return nil, fmt.Errorf("create catalog request: %w", err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("download catalog: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("download catalog: unexpected HTTP status %s", response.Status)
		}
		return readLimited(response.Body)
	}
	if strings.Contains(source, "://") {
		return nil, fmt.Errorf("unsupported catalog source %q", source)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open catalog: %w", err)
	}
	defer file.Close()
	return readLimited(file)
}

func readLimited(reader io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, maxCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	if len(payload) > maxCatalogBytes {
		return nil, fmt.Errorf("catalog exceeds %d bytes", maxCatalogBytes)
	}
	return payload, nil
}

func extractFeatureConfig(payload []byte) ([]byte, error) {
	data := payload
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("open catalog gzip: %w", err)
		}
		data, err = readLimited(reader)
		closeErr := reader.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close catalog gzip: %w", closeErr)
		}
	}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, tar.ErrHeader) || errors.Is(err, io.ErrUnexpectedEOF) {
			return data, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read catalog archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != "feature.cfg" {
			continue
		}
		return readLimited(reader)
	}
	return nil, errors.New("catalog archive does not contain feature.cfg")
}

func (c *Catalog) setError(err error) {
	c.mu.Lock()
	c.status.LastError = err.Error()
	c.mu.Unlock()
}

func timePtr(value time.Time) *time.Time {
	return &value
}
