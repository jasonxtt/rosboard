package diagnostics

import (
	"archive/zip"
	"bytes"
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
		"token=token123 secret=secret123",
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
	for _, forbidden := range []string{"abc123", "xxx", "cookie123", "wg-private", "token123", "secret123"} {
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

func mapValuesInOrder(entries map[string][]byte, paths []string) [][]byte {
	values := make([][]byte, 0, len(paths))
	for _, path := range paths {
		values = append(values, entries[path])
	}
	return values
}
