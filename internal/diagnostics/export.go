package diagnostics

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	maxExportZipBytes = 4 << 20
	maxExportLogBytes = 64 << 10
	maxExportLogLines = 500
)

var errExportTooLarge = errors.New("diagnostic export exceeds size limit")

type ExportManifest struct {
	Schema      string        `json:"schema"`
	GeneratedAt time.Time     `json:"generatedAt"`
	DeviceID    string        `json:"deviceId"`
	SourceMode  string        `json:"sourceMode"`
	Redaction   []string      `json:"redaction"`
	Limits      ExportLimits  `json:"limits"`
	Files       []ExportEntry `json:"files"`
}

type ExportLimits struct {
	MaxZipBytes int `json:"maxZipBytes"`
	MaxLogBytes int `json:"maxLogBytes"`
	MaxLogLines int `json:"maxLogLines"`
	MaxObjects  int `json:"maxObjectsPerEndpoint"`
}

type ExportEntry struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type exportFile struct {
	path string
	data []byte
}

// BuildDiagnosticExport creates the complete, bounded, read-only diagnostic
// package. The caller supplies the latest deep report and the already bounded
// runtime log tail; no credentials or config file are read here.
func BuildDiagnosticExport(report DeepReport, recentLogs string) ([]byte, string, error) {
	generatedAt := report.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	files := []exportFile{
		{path: "health-report.json", data: mustSanitizedJSON(report.Report)},
		{path: "routeros/snapshot.json", data: mustSanitizedJSON(report.Snapshot)},
		{path: "routeros/ingress-decision-trace.json", data: mustSanitizedJSON(report.IngressTrace)},
		{path: "monitor/status.json", data: mustSanitizedJSON(componentDocument(report.Report, "routeros.connection", "monitor.freshness"))},
		{path: "policy/status.json", data: mustSanitizedJSON(componentDocument(report.Report, "policy.state"))},
		{path: "access/status.json", data: mustSanitizedJSON(componentDocument(report.Report, "access.state"))},
		{path: "recognition/mosdns.json", data: mustSanitizedJSON(componentDocument(report.Report, "recognition.mosdns"))},
		{path: "update/status.json", data: mustSanitizedJSON(componentDocument(report.Report, "update.state"))},
		{path: "logs/recent.log", data: []byte(boundRecentLog(recentLogs))},
	}

	manifest := ExportManifest{
		Schema:      "rosboard.diagnostics/v1",
		GeneratedAt: generatedAt,
		DeviceID:    report.DeviceID,
		SourceMode:  report.Mode,
		Redaction: []string{
			"RouterOS password",
			"Authorization",
			"Cookie",
			"Session",
			"API token",
			"Secret",
			"WireGuard private key",
			"Other credentials and private keys",
		},
		Limits: ExportLimits{
			MaxZipBytes: maxExportZipBytes,
			MaxLogBytes: maxExportLogBytes,
			MaxLogLines: maxExportLogLines,
			MaxObjects:  maxSnapshotObjectReport,
		},
		Files: make([]ExportEntry, 0, len(files)),
	}
	for _, file := range files {
		digest := sha256.Sum256(file.data)
		manifest.Files = append(manifest.Files, ExportEntry{Path: file.path, Bytes: len(file.data), SHA256: hex.EncodeToString(digest[:])})
	}
	manifestData := mustSanitizedJSON(manifest)

	files = append([]exportFile{{path: "manifest.json", data: manifestData}}, files...)
	output := &boundedBuffer{max: maxExportZipBytes}
	archive := zip.NewWriter(output)
	for _, file := range files {
		entry, err := archive.Create(file.path)
		if err != nil {
			return nil, "", fmt.Errorf("create diagnostic entry %s: %w", file.path, err)
		}
		if _, err := entry.Write(file.data); err != nil {
			return nil, "", fmt.Errorf("write diagnostic entry %s: %w", file.path, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, "", fmt.Errorf("close diagnostic archive: %w", err)
	}
	filename := "rosboard-diagnostics-" + generatedAt.Format("20060102-150405") + ".zip"
	return output.data.Bytes(), filename, nil
}

func componentDocument(report Report, ids ...string) map[string]any {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	findings := make([]Finding, 0, len(ids))
	for _, finding := range report.Findings {
		if _, ok := wanted[finding.ID]; ok {
			findings = append(findings, finding)
		}
	}
	return map[string]any{
		"generatedAt": report.GeneratedAt,
		"deviceId":    report.DeviceID,
		"findings":    findings,
	}
}

func mustSanitizedJSON(value any) []byte {
	data, err := marshalSanitizedJSON(value)
	if err != nil {
		return []byte("{\"error\":\"failed to serialize diagnostic evidence\"}\n")
	}
	return data
}

func marshalSanitizedJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	clean := sanitizeJSONValue("", decoded)
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func sanitizeJSONValue(key string, value any) any {
	if sensitiveKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			clean[childKey] = sanitizeJSONValue(childKey, childValue)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, childValue := range typed {
			clean[index] = sanitizeJSONValue("", childValue)
		}
		return clean
	case string:
		return redactSensitiveText(typed)
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(key))
	for _, marker := range []string{
		"password", "passwd", "passphrase", "authorization", "cookie", "session",
		"token", "secret", "privatekey", "presharedkey", "apikey", "credential",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

var (
	bearerPattern     = regexp.MustCompile(`(?i)\bBearer\s+[^\s,;]+`)
	basicPattern      = regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]+`)
	assignmentPattern = regexp.MustCompile(`(?i)(password|passwd|passphrase|authorization|cookie|session|token|secret|private[-_ ]?key|preshared[-_ ]?key|api[-_ ]?key)(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
)

func redactSensitiveText(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = basicPattern.ReplaceAllString(value, "Basic [REDACTED]")
	return assignmentPattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

func boundRecentLog(value string) string {
	value = redactSensitiveText(value)
	lines := strings.Split(value, "\n")
	if len(lines) > maxExportLogLines {
		lines = lines[len(lines)-maxExportLogLines:]
	}
	value = strings.Join(lines, "\n")
	if len(value) > maxExportLogBytes {
		value = value[len(value)-maxExportLogBytes:]
	}
	if value == "" {
		return "No file-backed runtime log source was configured.\n"
	}
	return value
}

type boundedBuffer struct {
	data bytes.Buffer
	max  int
}

func (b *boundedBuffer) Write(payload []byte) (int, error) {
	if b.data.Len()+len(payload) > b.max {
		return 0, errExportTooLarge
	}
	return b.data.Write(payload)
}
