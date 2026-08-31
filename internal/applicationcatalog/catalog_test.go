package applicationcatalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseOAFKeepsStableIDAndOnlySafeDomains(t *testing.T) {
	parsed, err := parsePayload([]byte(`#version v25.07.03
#format v3.0
001101 Safe App:[tcp;;;Example.COM;;,udp;;;second.example;;,tcp;;;example.com;;]
1102 HostAndRequest:[tcp;;;request.example;/path;;]
1103 Port:[tcp;;80;port.example;;]
1104 Dictionary:[tcp;;;dict.example;;key=value;]
1105 Search:[tcp;;;search.example;;;;needle]
`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Version != "v25.07.03" || len(parsed.Applications) != 1 {
		t.Fatalf("unexpected parsed catalog: %#v", parsed)
	}
	application := parsed.Applications[0]
	if application.ID != "oaf:1101" || application.Name != "Safe App" {
		t.Fatalf("unexpected stable application identity: %#v", application)
	}
	if strings.Join(application.DomainSignatures, ",") != "example.com,second.example" {
		t.Fatalf("unsafe signatures were not excluded: %#v", application.DomainSignatures)
	}
}

func TestLookupUsesExactLongestSuffixAndAmbiguousResult(t *testing.T) {
	parsed, err := parsePayload([]byte(`#version v1
#format v3.0
1101 Broad:[tcp;;;example.com;;]
1102 Narrow:[tcp;;;api.example.com;;]
1103 One:[tcp;;;shared.example;;]
1104 Two:[tcp;;;shared.example;;]
`))
	if err != nil {
		t.Fatal(err)
	}
	catalog := &Catalog{current: makeSnapshot(parsed.Applications)}

	tests := []struct {
		name          string
		domain        string
		applicationID string
		matchedDomain string
	}{
		{name: "exact match wins", domain: "API.Example.COM.", applicationID: "oaf:1102", matchedDomain: "api.example.com"},
		{name: "longest suffix", domain: "deep.api.example.com", applicationID: "oaf:1102", matchedDomain: "api.example.com"},
		{name: "parent suffix", domain: "www.example.com", applicationID: "oaf:1101", matchedDomain: "example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match := catalog.LookupDomain(test.domain)
			if match.Ambiguous || match.Application.ID != test.applicationID || match.MatchedDomain != test.matchedDomain {
				t.Fatalf("LookupDomain(%q)=%+v", test.domain, match)
			}
		})
	}

	ambiguous := catalog.LookupDomain("shared.example")
	if !ambiguous.Ambiguous || ambiguous.MatchedDomain != "shared.example" || ambiguous.Application.ID != "" {
		t.Fatalf("ambiguous lookup must not choose an application: %+v", ambiguous)
	}
	if match := catalog.LookupDomain("notexample.com"); match.Ambiguous || match.MatchedDomain != "" || match.Application.ID != "" {
		t.Fatalf("suffix matching crossed a label boundary: %+v", match)
	}
	if match := catalog.LookupDomain("bad..example"); match.Ambiguous || match.MatchedDomain != "" || match.Application.ID != "" {
		t.Fatalf("invalid domain unexpectedly matched: %+v", match)
	}
}

func TestRefreshKeepsLastGoodSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feature.cfg")
	if err := os.WriteFile(path, []byte(`#version v1
#format v3.0
1101 First:[tcp;;;first.example;;]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := New(path, time.Hour)
	if status := catalog.Status(); status.LastSuccess != nil || status.ApplicationCount != 0 {
		t.Fatalf("catalog should start unavailable: %+v", status)
	}
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := catalog.Status()
	if status.LastSuccess == nil || status.Version != "v1" || status.ApplicationCount != 1 || status.DomainCount != 1 {
		t.Fatalf("unexpected initial catalog status: %+v", status)
	}

	if err := os.WriteFile(path, []byte(`#version v2
#format v3.0
1101 Renamed:[tcp;;;first.example;;]
1102 Second:[tcp;;;second.example;;]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if match := catalog.LookupDomain("first.example"); match.Application.ID != "oaf:1101" || match.Application.Name != "Renamed" {
		t.Fatalf("successful refresh did not replace snapshot: %+v", match)
	}
	if application, ok := catalog.Get(" oaf:1101 "); !ok || application.Name != "Renamed" {
		t.Fatalf("catalog Get did not return the stable application: %+v, %v", application, ok)
	}
	if _, ok := catalog.Get("missing"); ok {
		t.Fatal("catalog Get unexpectedly returned a missing application")
	}

	if err := os.WriteFile(path, []byte("not an OAF application\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Refresh(context.Background()); err == nil {
		t.Fatal("malformed refresh unexpectedly succeeded")
	}
	status = catalog.Status()
	if status.LastSuccess == nil || status.Version != "v2" || status.ApplicationCount != 2 || status.LastError == "" {
		t.Fatalf("failed refresh should retain last-good status: %+v", status)
	}
	if match := catalog.LookupDomain("second.example"); match.Application.ID != "oaf:1102" {
		t.Fatalf("failed refresh replaced last-good snapshot: %+v", match)
	}
}

func TestRefreshLoadsGzippedFeatureConfigFromHTTP(t *testing.T) {
	payload := []byte(`#version v3
#format v3.0
1201 Remote:[tcp;;;remote.example;;]
`)
	archive := gzipTar(t, payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(archive)
	}))
	defer server.Close()

	catalog := New(server.URL, time.Hour)
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if match := catalog.LookupDomain("remote.example"); match.Application.ID != "oaf:1201" {
		t.Fatalf("HTTP archive was not loaded: %+v", match)
	}
}

func gzipTar(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "release/feature.cfg", Mode: 0o600, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
