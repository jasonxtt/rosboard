package diagnostics

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"rosboard/internal/policyv2"
)

func TestBuildDiagnosticExportRedactsSensitiveValuesAndIncludesExpectedTree(t *testing.T) {
	report := DeepReport{
		Report: Report{
			GeneratedAt: time.Date(2026, time.September, 10, 8, 9, 10, 0, time.UTC),
			Mode:        ModeDeep,
			DeviceID:    "edge",
			Overall:     OverallHealthy,
			Findings: []Finding{{
				ID:     "fixture",
				Group:  "routeros",
				Status: StatusOK,
				Evidence: map[string]any{
					"safeAddress":   "192.0.2.1",
					"password":      "abc123",
					"authorization": "Bearer xxx",
					"private-key":   "wg-private",
					"nestedMessage": "token=token123 secret=secret123",
				},
			}},
		},
		Snapshot: EvidenceSnapshot{
			CapturedAt:  time.Date(2026, time.September, 10, 8, 9, 10, 0, time.UTC),
			Fingerprint: "fixture-fingerprint",
			Endpoints: []EndpointSnapshot{{
				Endpoint:    "/interface",
				ObjectCount: 1,
				ReadCount:   1,
				Objects: []map[string]string{{
					"name":         "bridge-lan",
					"password":     "abc123",
					"comment":      "Authorization: Bearer xxx private-key=wg-private",
					"safe-address": "192.0.2.1",
				}},
			}},
		},
		IngressTrace: []policyv2.IngressDecision{{
			Interface:  "bridge-lan",
			Result:     "accepted",
			ReasonCode: "ingress.accepted",
			Evidence:   map[string]any{"cookie": "cookie123", "address": "192.0.2.1"},
		}},
	}
	recentLogs := strings.Join([]string{
		"password=abc123",
		"Authorization: Bearer xxx",
		"Cookie: session=cookie123",
		"private-key=wg-private",
		"token=token123 secret=secret123 credential=credential123",
	}, "\n")

	archiveData, filename, err := BuildDiagnosticExport(report, recentLogs)
	if err != nil {
		t.Fatalf("BuildDiagnosticExport() error = %v", err)
	}
	if filename != "rosboard-diagnostics-20260910-080910.zip" {
		t.Fatalf("filename = %q", filename)
	}
	entries := readDiagnosticArchive(t, archiveData)
	wantPaths := []string{
		"manifest.json",
		"health-report.json",
		"routeros/snapshot.json",
		"routeros/ingress-decision-trace.json",
		"monitor/status.json",
		"policy/status.json",
		"access/status.json",
		"recognition/mosdns.json",
		"update/status.json",
		"logs/recent.log",
	}
	for _, path := range wantPaths {
		if _, ok := entries[path]; !ok {
			t.Fatalf("archive is missing %q; got %v", path, sortedDiagnosticKeys(entries))
		}
	}
	joined := string(bytes.Join(mapValuesInOrder(entries, wantPaths), []byte("\n")))
	for _, forbidden := range []string{"abc123", "xxx", "cookie123", "wg-private", "token123", "secret123", "credential123"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("archive contains forbidden fixture value %q", forbidden)
		}
	}
	if !strings.Contains(joined, "192.0.2.1") || !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("archive lost allowed evidence or redaction marker: %s", joined)
	}
}

func TestBuildDiagnosticExportBoundsRecentLogs(t *testing.T) {
	var logs strings.Builder
	for index := 0; index < maxExportLogLines+100; index++ {
		_, _ = fmt.Fprintf(&logs, "safe-line-%d\n", index)
	}
	archiveData, _, err := BuildDiagnosticExport(DeepReport{Report: Report{GeneratedAt: time.Now().UTC(), Mode: ModeDeep, DeviceID: "edge"}}, logs.String())
	if err != nil {
		t.Fatalf("BuildDiagnosticExport() error = %v", err)
	}
	entries := readDiagnosticArchive(t, archiveData)
	lines := strings.Split(strings.TrimSuffix(string(entries["logs/recent.log"]), "\n"), "\n")
	if len(lines) > maxExportLogLines {
		t.Fatalf("log lines = %d, want at most %d", len(lines), maxExportLogLines)
	}
	if len(entries["logs/recent.log"]) > maxExportLogBytes {
		t.Fatalf("log bytes = %d, want at most %d", len(entries["logs/recent.log"]), maxExportLogBytes)
	}
}

func TestBuildDiagnosticExportRedactsNetworkAndIdentityPrivacy(t *testing.T) {
	report := DeepReport{
		Report: Report{
			GeneratedAt: time.Date(2026, time.September, 10, 8, 9, 10, 0, time.UTC),
			Mode:        ModeDeep,
			DeviceID:    "customer-secret-device-uuid",
			Overall:     OverallHealthy,
			Findings: []Finding{{
				ID:     "privacy-fixture",
				Group:  "routeros",
				Status: StatusOK,
				Evidence: map[string]any{
					"globalPrefix":      "2409:8a6c:6910:3e0::/60",
					"globalHost":        "2409:8a6c:6910:3e0:1234:5678:9abc:def0/64",
					"linkLocal":         "fe80::20c:29ff:fe98:8b73/64",
					"linkZone":          "fe80::1234:5678%ether2",
					"ula":               "fd86:1234:5678::abcd/64",
					"publicIPv4":        "123.45.67.89 123.45.67.0/24",
					"privateIPv4":       "10.0.0.99/24 127.0.0.1 169.254.1.1 100.64.0.7",
					"specialRoutes":     "0.0.0.0/0 ::/0 ::1 ::1/128 ff02::1",
					"metadata":          "version=1.2.3.4 sha=0123456789abcdef timestamp=2026-09-10T08:09:10Z",
					"embeddedEndpoints": "dial tcp 123.45.67.89:8728 http://123.45.67.89:8728/api 123.45.67.89%ether1",
					"dataDir":           "/home/alice/rosboard/data",
				},
			}},
		},
		Snapshot: EvidenceSnapshot{
			CapturedAt:  time.Date(2026, time.September, 10, 8, 9, 10, 0, time.UTC),
			Fingerprint: "privacy-fixture-fingerprint",
			Endpoints: []EndpointSnapshot{{
				Endpoint:    "/ipv6/address",
				ObjectCount: 1,
				ReadCount:   1,
				Objects: []map[string]string{{
					"global":     "2409:8a6c:6910:3e0::/60",
					"link-local": "fe80::20c:29ff:fe98:8b73/64",
					"ula":        "fd86:1234:5678::abcd/64",
					"public":     "123.45.67.0/24",
					"private":    "10.0.0.99/24",
					"dataDir":    "/home/alice/rosboard/data",
				}},
			}},
		},
		IngressTrace: []policyv2.IngressDecision{{
			Interface:  "ether2",
			Result:     "accepted",
			ReasonCode: "ingress.accepted",
			Evidence: map[string]any{
				"client":  "2409:8a6c:6910:3e0::/64",
				"gateway": "123.45.67.89",
				"peer":    "fe80::1234:5678%ether2",
			},
		}},
	}
	recentLogs := strings.Join([]string{
		"client=2409:8a6c:6910:3e0::/64 gateway=123.45.67.89",
		"peer=fe80::1234:5678%ether2 ula=fd86:1234:5678::abcd/64",
		"dataDir=/home/alice/rosboard/data deviceId=customer-secret-device-uuid",
		"rosboard 2026/09/10 12:05:51 serving on 0.0.0.0:80 using data dir /home/alice/rosboard/data",
		"device customer-secret-device-uuid background refresh failed: dial tcp 123.45.67.89:8728",
		"password=privacy-password-fixture Authorization: Bearer privacy-authorization-fixture Cookie: session=privacy-cookie-fixture token=privacy-token-fixture secret=privacy-secret-fixture private-key=privacy-private-key-fixture credential=privacy-credential-fixture",
	}, "\n")

	archiveData, _, err := BuildDiagnosticExport(report, recentLogs)
	if err != nil {
		t.Fatalf("BuildDiagnosticExport() error = %v", err)
	}
	entries := readDiagnosticArchive(t, archiveData)
	joined := string(bytes.Join(mapValuesInOrder(entries, []string{
		"manifest.json",
		"health-report.json",
		"routeros/snapshot.json",
		"routeros/ingress-decision-trace.json",
		"monitor/status.json",
		"policy/status.json",
		"access/status.json",
		"recognition/mosdns.json",
		"update/status.json",
		"logs/recent.log",
	}), []byte("\n")))
	for _, forbidden := range []string{
		"2409:8a6c:6910:3e0::/60",
		"2409:8a6c:6910:3e0:1234:5678:9abc:def0/64",
		"fe80::20c:29ff:fe98:8b73/64",
		"fe80::1234:5678%ether2",
		"fd86:1234:5678::abcd/64",
		"123.45.67.89",
		"123.45.67.0/24",
		"customer-secret-device-uuid",
		"/home/alice/rosboard/data",
		"privacy-password-fixture",
		"privacy-authorization-fixture",
		"privacy-cookie-fixture",
		"privacy-token-fixture",
		"privacy-secret-fixture",
		"privacy-private-key-fixture",
		"privacy-credential-fixture",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("archive contains forbidden value %q", forbidden)
		}
	}
	for _, expected := range []string{
		"2409:xxxx:xxxx:xxxx::/60",
		"2409:xxxx:xxxx:xxxx::/64",
		"fe80::xxxx/64",
		"fe80::xxxx%ether2",
		"fd86::xxxx/64",
		"123.45.x.x",
		"123.45.x.x/24",
		"123.45.x.x:8728",
		"http://123.45.x.x:8728/api",
		"123.45.x.x%ether1",
		"10.0.0.99/24",
		"127.0.0.1",
		"169.254.1.1",
		"100.64.0.7",
		"0.0.0.0/0",
		"::/0",
		"::1",
		"::1/128",
		"ff02::1",
		redactedDeviceID,
		"[REDACTED_PATH]/data",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("archive is missing expected sanitized value %q", expected)
		}
	}
	if !strings.Contains(string(entries["logs/recent.log"]), "using data dir [REDACTED_PATH]/data") {
		t.Fatalf("recent log did not redact natural-language data dir: %s", entries["logs/recent.log"])
	}
	if !strings.Contains(joined, "version=1.2.3.4") || !strings.Contains(joined, "timestamp=2026-09-10T08:09:10Z") {
		t.Fatalf("archive changed version or timestamp metadata: %s", joined)
	}

	var manifest struct {
		Redaction []string `json:"redaction"`
		Files     []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	for _, category := range []string{"Public IPv4", "Global IPv6", "IPv6 interface identifiers", "Device identifier", "Local absolute data path"} {
		if !containsString(manifest.Redaction, category) {
			t.Fatalf("manifest redaction list missing %q: %#v", category, manifest.Redaction)
		}
	}
	for _, file := range manifest.Files {
		data, ok := entries[file.Path]
		if !ok {
			t.Fatalf("manifest references missing entry %q", file.Path)
		}
		if len(data) != file.Bytes {
			t.Fatalf("manifest byte count for %q = %d, want %d", file.Path, len(data), file.Bytes)
		}
		digest := sha256.Sum256(data)
		if got := hex.EncodeToString(digest[:]); got != file.SHA256 {
			t.Fatalf("manifest hash for %q = %s, want %s", file.Path, got, file.SHA256)
		}
	}
}

func TestBuildDiagnosticExportDoesNotMutateDeepReport(t *testing.T) {
	report := DeepReport{
		Report: Report{
			GeneratedAt: time.Date(2026, time.September, 10, 8, 9, 10, 0, time.UTC),
			Mode:        ModeDeep,
			DeviceID:    "customer-secret-device-uuid",
			Findings: []Finding{{
				ID:       "fixture",
				Evidence: map[string]any{"address": "2409:8a6c:6910:3e0::/60", "dataDir": "/home/alice/rosboard/data"},
			}},
		},
	}
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	if _, _, err := BuildDiagnosticExport(report, ""); err != nil {
		t.Fatalf("BuildDiagnosticExport() error = %v", err)
	}
	after, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("BuildDiagnosticExport mutated the deep report: before=%s after=%s", before, after)
	}
}

func TestBuildDiagnosticExportRejectsOversizedArchive(t *testing.T) {
	noise := make([]byte, maxExportZipBytes*2)
	state := uint32(0x12345678)
	for index := range noise {
		state = state*1664525 + 1013904223
		noise[index] = byte(state >> 24)
	}
	report := DeepReport{
		Report: Report{
			GeneratedAt: time.Now().UTC(),
			Mode:        ModeDeep,
			DeviceID:    "edge",
			Findings: []Finding{{
				ID:       "fixture",
				Evidence: map[string]any{"large": string(noise)},
			}},
		},
	}
	if _, _, err := BuildDiagnosticExport(report, ""); !errors.Is(err, errExportTooLarge) {
		t.Fatalf("BuildDiagnosticExport() error = %v, want %v", err, errExportTooLarge)
	}
}

func TestLogBufferKeepsBoundedTail(t *testing.T) {
	buffer := NewLogBuffer(3, 32)
	if _, err := buffer.Write([]byte("one\ntwo\nthree\nfour\n")); err != nil {
		t.Fatalf("LogBuffer.Write() error = %v", err)
	}
	got := buffer.Recent()
	if strings.Contains(got, "one") || !strings.Contains(got, "four") {
		t.Fatalf("Recent() = %q, want bounded tail", got)
	}
}

func readDiagnosticArchive(t *testing.T, payload []byte) map[string][]byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	entries := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", file.Name, readErr)
		}
		entries[file.Name] = data
	}
	return entries
}

func sortedDiagnosticKeys(entries map[string][]byte) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	return keys
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mapValuesInOrder(entries map[string][]byte, paths []string) [][]byte {
	values := make([][]byte, 0, len(paths))
	for _, path := range paths {
		values = append(values, entries[path])
	}
	return values
}
