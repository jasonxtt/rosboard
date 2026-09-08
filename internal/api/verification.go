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
	"rosboard/internal/model"
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

func (t *verificationTickets) lookup(token string) (verificationTicket, error) {
	tokenHash, err := verificationTokenHash(token)
	if err != nil {
		return verificationTicket{}, errVerificationRequired
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked()
	ticket, ok := t.items[tokenHash]
	if !ok || !ticket.expiresAt.After(t.now().UTC()) {
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

type deviceVerificationResponse struct {
	VerificationToken string                           `json:"verificationToken"`
	ExpiresAt         time.Time                        `json:"expiresAt"`
	Identity          routeros.VerificationIdentity    `json:"identity"`
	Interfaces        []routeros.VerificationInterface `json:"interfaces"`
	CIDRCandidates    []routeros.CIDRCandidate         `json:"cidrCandidates"`
	TrafficScope      model.TrafficScope               `json:"trafficScope"`
	TerminalScope     model.TerminalScope              `json:"terminalScope"`
	Warnings          []routeros.VerificationWarning   `json:"warnings"`
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
	_, _, terminalScope, trafficScope, err := verificationScopes(result, payload.TrafficScope, payload.TerminalScope)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	fingerprint := connectionFingerprint(baseURL, payload.Username, password)
	token, expiresAt, err := s.tickets.issueWithTopology(fingerprint, result.Interfaces, result.Topology)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "verification_failed", "failed to create verification ticket")
		return
	}
	writeJSON(writer, http.StatusOK, deviceVerificationResponse{
		VerificationToken: token, ExpiresAt: expiresAt, Identity: result.Identity,
		Interfaces: result.Interfaces, CIDRCandidates: result.CIDRCandidates,
		TrafficScope: trafficScope, TerminalScope: terminalScope, Warnings: result.Warnings,
	})
}

func verificationScopes(result routeros.VerificationResult, trafficConfig config.TrafficScopeConfig, terminalConfig config.TerminalScopeConfig) (config.TrafficScopeConfig, config.TerminalScopeConfig, model.TerminalScope, model.TrafficScope, error) {
	var err error
	if trafficConfig, err = canonicalTrafficScope(trafficConfig); err != nil {
		return config.TrafficScopeConfig{}, config.TerminalScopeConfig{}, model.TerminalScope{}, model.TrafficScope{}, err
	}
	if terminalConfig, err = canonicalTerminalScope(terminalConfig); err != nil {
		return config.TrafficScopeConfig{}, config.TerminalScopeConfig{}, model.TerminalScope{}, model.TrafficScope{}, err
	}
	if err := validateTrafficScopeInterfaces(trafficConfig, verificationInterfaceSet(result.Interfaces)); err != nil {
		return config.TrafficScopeConfig{}, config.TerminalScopeConfig{}, model.TerminalScope{}, model.TrafficScope{}, err
	}
	terminalScope, trafficScope := service.PreviewScopes(config.RouterOSConfig{TrafficScope: trafficConfig, TerminalScope: terminalConfig}, result.Topology)
	return trafficConfig, terminalConfig, terminalScope, trafficScope, nil
}

type scopePreviewRequest struct {
	VerificationToken string                     `json:"verificationToken"`
	TrafficScope      config.TrafficScopeConfig  `json:"trafficScope"`
	TerminalScope     config.TerminalScopeConfig `json:"terminalScope"`
}

func (s *Server) serveDeviceScopePreview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var payload scopePreviewRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	ticket, err := s.tickets.lookup(payload.VerificationToken)
	if err != nil {
		writeAPIError(writer, http.StatusConflict, "verification_required", err.Error())
		return
	}
	result := routeros.VerificationResult{Interfaces: make([]routeros.VerificationInterface, 0, len(ticket.interfaces)), Topology: ticket.topology}
	for name := range ticket.interfaces {
		result.Interfaces = append(result.Interfaces, routeros.VerificationInterface{Name: name})
	}
	_, _, terminalScope, trafficScope, err := verificationScopes(result, payload.TrafficScope, payload.TerminalScope)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"trafficScope": trafficScope, "terminalScope": terminalScope})
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
