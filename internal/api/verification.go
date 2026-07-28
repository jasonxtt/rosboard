package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"rosboard/internal/service"
)

const verificationLifetime = 15 * time.Minute

var errVerificationRequired = errors.New("connection verification is required")

type verificationTicket struct {
	fingerprint [32]byte
	interfaces  map[string]struct{}
	topology    routeros.TopologySnapshot
	expiresAt   time.Time
}

type verificationTickets struct {
	mu     sync.Mutex
	now    func() time.Time
	random io.Reader
	items  map[[32]byte]verificationTicket
}

func newVerificationTickets() *verificationTickets {
	return &verificationTickets{now: time.Now, random: rand.Reader, items: make(map[[32]byte]verificationTicket)}
}

func (t *verificationTickets) issue(fingerprint [32]byte, interfaces []routeros.VerificationInterface) (string, time.Time, error) {
	return t.issueWithTopology(fingerprint, interfaces, routeros.TopologySnapshot{})
}

func (t *verificationTickets) issueWithTopology(fingerprint [32]byte, interfaces []routeros.VerificationInterface, topology routeros.TopologySnapshot) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(t.random, raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate verification token: %w", err)
	}
	tokenHash := sha256.Sum256(raw)
	expiresAt := t.now().UTC().Add(verificationLifetime)
	allowed := make(map[string]struct{}, len(interfaces))
	for _, item := range interfaces {
		allowed[item.Name] = struct{}{}
	}
	t.mu.Lock()
	t.pruneLocked()
	t.items[tokenHash] = verificationTicket{fingerprint: fingerprint, interfaces: allowed, topology: topology, expiresAt: expiresAt}
	t.mu.Unlock()
	return base64.RawURLEncoding.EncodeToString(raw), expiresAt, nil
}

func (t *verificationTickets) validate(token string, fingerprint [32]byte) (verificationTicket, error) {
	tokenHash, err := verificationTokenHash(token)
	if err != nil {
		return verificationTicket{}, errVerificationRequired
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked()
	ticket, ok := t.items[tokenHash]
	if !ok || ticket.fingerprint != fingerprint || !ticket.expiresAt.After(t.now().UTC()) {
		return verificationTicket{}, errVerificationRequired
	}
	return ticket, nil
}

func (t *verificationTickets) consume(token string) {
	tokenHash, err := verificationTokenHash(token)
	if err != nil {
		return
	}
	t.mu.Lock()
	delete(t.items, tokenHash)
	t.mu.Unlock()
}

func (t *verificationTickets) clear() {
	t.mu.Lock()
	clear(t.items)
	t.mu.Unlock()
}

func (t *verificationTickets) pruneLocked() {
	now := t.now().UTC()
	for key, ticket := range t.items {
		if !ticket.expiresAt.After(now) {
			delete(t.items, key)
		}
	}
}

func verificationTokenHash(token string) ([32]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil || len(raw) != 32 {
		return [32]byte{}, errVerificationRequired
	}
	return sha256.Sum256(raw), nil
}

func connectionFingerprint(baseURL, username, password string) [32]byte {
	return sha256.Sum256([]byte(baseURL + "\x00" + strings.TrimSpace(username) + "\x00" + password))
}

type connectionTestRequest struct {
	DeviceID      string                     `json:"deviceId"`
	Scheme        string                     `json:"scheme"`
	Host          string                     `json:"host"`
	Port          int                        `json:"port"`
	Username      string                     `json:"username"`
	Password      string                     `json:"password"`
	TrafficScope  config.TrafficScopeConfig  `json:"trafficScope"`
	TerminalScope config.TerminalScopeConfig `json:"terminalScope"`
}

func (s *Server) serveDeviceConnectionTest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var payload connectionTestRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	baseURL, password, err := s.resolveTestConnection(payload)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_connection", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 25*time.Second)
	defer cancel()
	client := routeros.NewClient(baseURL, strings.TrimSpace(payload.Username), password)
	result, err := client.Verify(ctx)
	if err != nil {
		var verificationErr *routeros.VerificationError
		if errors.As(err, &verificationErr) {
			writeAPIError(writer, http.StatusBadGateway, "routeros_"+verificationErr.Kind, verificationErr.Message)
		} else {
			writeAPIError(writer, http.StatusBadGateway, "routeros_connection", "Unable to verify RouterOS connection.")
		}
		return
	}
	trafficConfig, err := canonicalTrafficScope(payload.TrafficScope)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	terminalConfig, err := canonicalTerminalScope(payload.TerminalScope)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	allowed := verificationInterfaceSet(result.Interfaces)
	if err := validateTrafficScopeInterfaces(trafficConfig, allowed); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	previewConfig := config.RouterOSConfig{TrafficScope: trafficConfig, TerminalScope: terminalConfig}
	terminalScope, trafficScope := service.PreviewScopes(previewConfig, result.Topology)
	fingerprint := connectionFingerprint(baseURL, payload.Username, password)
	token, expiresAt, err := s.tickets.issueWithTopology(fingerprint, result.Interfaces, result.Topology)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "verification_failed", "failed to create verification ticket")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"verificationToken": token, "expiresAt": expiresAt, "identity": result.Identity,
		"interfaces": result.Interfaces, "cidrCandidates": result.CIDRCandidates, "trafficScope": trafficScope, "terminalScope": terminalScope, "warnings": result.Warnings,
	})
}

func (s *Server) resolveTestConnection(payload connectionTestRequest) (string, string, error) {
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		return "", "", errors.New("username is required")
	}
	password := payload.Password
	if strings.TrimSpace(payload.DeviceID) != "" && password == "" {
		device, found := s.configSnapshot().Device(strings.TrimSpace(payload.DeviceID))
		if !found {
			return "", "", errors.New("device not found")
		}
		password = device.RouterOS.Password
	}
	if password == "" {
		return "", "", errors.New("password is required")
	}
	baseURL, err := normalizedRouterOSURL(payload.Scheme, payload.Host, payload.Port)
	if err != nil {
		return "", "", err
	}
	return baseURL, password, nil
}

func validateSelectedInterfaces(names []string, allowed map[string]struct{}) ([]string, error) {
	cleaned := cleanStrings(names)
	if len(cleaned) == 0 {
		return nil, errors.New("at least one traffic interface is required")
	}
	for _, name := range cleaned {
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("traffic interface %q is unavailable", name)
		}
	}
	sort.Strings(cleaned)
	return cleaned, nil
}

func validateTrafficScopeInterfaces(scope config.TrafficScopeConfig, allowed map[string]struct{}) error {
	for _, name := range scope.IncludeInterfaces {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("traffic scope include interface %q is unavailable", name)
		}
	}
	for _, name := range scope.ExcludeInterfaces {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("traffic scope exclude interface %q is unavailable", name)
		}
	}
	return nil
}

func verificationInterfaceSet(interfaces []routeros.VerificationInterface) map[string]struct{} {
	allowed := make(map[string]struct{}, len(interfaces))
	for _, item := range interfaces {
		if name := strings.TrimSpace(item.Name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	return allowed
}
