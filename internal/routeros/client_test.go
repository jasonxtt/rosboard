package routeros

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSystemHealthAcceptsRouterOSObjectAndArrayResponses(t *testing.T) {
	for name, body := range map[string]string{
		"array":  `[{"state":"enabled","state-after-reboot":"enabled"}]`,
		"object": `{"state":"enabled","state-after-reboot":"enabled"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/rest/system/health" {
					t.Fatalf("path = %q", request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()

			health, err := NewClient(server.URL, "admin", "secret").SystemHealth(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if health.State != "enabled" || health.StateAfterReboot != "enabled" {
				t.Fatalf("unexpected health: %#v", health)
			}
		})
	}
}
