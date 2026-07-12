package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/service"
)

func TestViewerHeartbeatRequiresPostAndReturnsDeadline(t *testing.T) {
	monitor := service.NewMonitor(config.Config{}, nil, nil, log.Default())
	server := NewServer(config.Config{}, monitor, nil)

	getResponse := httptest.NewRecorder()
	server.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/viewer-heartbeat", nil))
	if getResponse.Code != http.StatusMethodNotAllowed || getResponse.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status=%d allow=%q", getResponse.Code, getResponse.Header().Get("Allow"))
	}

	before := time.Now()
	postResponse := httptest.NewRecorder()
	server.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/api/viewer-heartbeat", nil))
	if postResponse.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", postResponse.Code, postResponse.Body.String())
	}
	var payload struct {
		ActiveUntil time.Time `json:"activeUntil"`
	}
	if err := json.Unmarshal(postResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.ActiveUntil.After(before.Add(20 * time.Second)) {
		t.Fatalf("heartbeat deadline was not extended: %s", payload.ActiveUntil)
	}
}
