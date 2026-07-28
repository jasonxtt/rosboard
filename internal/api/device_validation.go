package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

func writeDeviceValidationError(writer http.ResponseWriter, err error) {
	if errors.Is(err, errVerificationRequired) {
		writeAPIError(writer, http.StatusConflict, "verification_required", err.Error())
		return
	}
	writeAPIError(writer, http.StatusBadRequest, "invalid_device", err.Error())
}

func (s *Server) prepareDevice(ctx context.Context, id string, payload deviceSettingsRequest, existing *config.DeviceConfig) (config.DeviceConfig, bool, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return config.DeviceConfig{}, false, errors.New("device name is required")
	}
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		return config.DeviceConfig{}, false, errors.New("username is required")
	}
	password := payload.Password
	if password == "" && existing != nil {
		password = existing.RouterOS.Password
	}
	if password == "" {
		return config.DeviceConfig{}, false, errors.New("password is required")
	}
	baseURL, err := normalizedRouterOSURL(payload.Scheme, payload.Host, payload.Port)
	if err != nil {
		return config.DeviceConfig{}, false, err
	}
	cidrs, err := canonicalCIDRs(payload.TerminalCIDRs)
	if err != nil {
		return config.DeviceConfig{}, false, err
	}
	scope, err := canonicalTerminalScope(payload.TerminalScope)
	if err != nil {
		return config.DeviceConfig{}, false, err
	}
	if err := s.rejectDuplicateEndpoint(id, baseURL); err != nil {
		return config.DeviceConfig{}, false, err
	}

	connectionChanged := existing == nil || existing.RouterOS.BaseURL != baseURL || existing.RouterOS.Username != username || existing.RouterOS.Password != password
	allowed := make(map[string]struct{})
	consumeTicket := false
	if connectionChanged {
		ticket, err := s.tickets.validate(payload.VerificationToken, connectionFingerprint(baseURL, username, password))
		if err != nil {
			return config.DeviceConfig{}, false, err
		}
		allowed = ticket.interfaces
		consumeTicket = true
	} else {
		verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		interfaces, err := routeros.NewClient(baseURL, username, password).Interfaces(verifyCtx)
		if err != nil {
			return config.DeviceConfig{}, false, errors.New("unable to refresh RouterOS interfaces")
		}
		for _, item := range interfaces {
			if value := strings.TrimSpace(item.Name); value != "" {
				allowed[value] = struct{}{}
			}
		}
	}
	interfaces, err := validateSelectedInterfaces(payload.TrafficInterfaces, allowed)
	if err != nil {
		return config.DeviceConfig{}, false, err
	}
	trafficCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := routeros.NewClient(baseURL, username, password).VerifyTrafficInterfaces(trafficCtx, interfaces); err != nil {
		return config.DeviceConfig{}, false, err
	}

	device := config.DeviceConfig{
		ID: id, Name: name, Enabled: payload.Enabled,
		RouterOS: config.RouterOSConfig{
			BaseURL: baseURL, Username: username, Password: password,
			TrafficInterfaces: interfaces, TerminalCIDRs: cidrs, TerminalScope: scope,
		},
	}
	if existing != nil {
		device.Archived = existing.Archived
	}
	return device, consumeTicket, nil
}

func canonicalCIDRs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", value)
		}
		canonical := network.String()
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result, nil
}

func canonicalTerminalScope(scope config.TerminalScopeConfig) (config.TerminalScopeConfig, error) {
	scope.Mode = strings.ToLower(strings.TrimSpace(scope.Mode))
	if scope.Mode != "" && scope.Mode != "auto" {
		return config.TerminalScopeConfig{}, errors.New("terminal scope mode must be auto")
	}
	scope.IncludeInterfaces = normalizedStrings(scope.IncludeInterfaces)
	scope.ExcludeInterfaces = normalizedStrings(scope.ExcludeInterfaces)
	for _, name := range scope.IncludeInterfaces {
		for _, excluded := range scope.ExcludeInterfaces {
			if strings.EqualFold(name, excluded) {
				return config.TerminalScopeConfig{}, fmt.Errorf("interface %q is both included and excluded", name)
			}
		}
	}
	var err error
	if scope.IncludeCIDRs, err = canonicalCIDRs(scope.IncludeCIDRs); err != nil {
		return config.TerminalScopeConfig{}, err
	}
	if scope.ExcludeCIDRs, err = canonicalCIDRs(scope.ExcludeCIDRs); err != nil {
		return config.TerminalScopeConfig{}, err
	}
	return scope, nil
}

func normalizedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value != "" {
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}

func (s *Server) rejectDuplicateEndpoint(id, baseURL string) error {
	for _, candidate := range s.configSnapshot().Devices {
		if candidate.ID == id {
			continue
		}
		scheme, host, port := routerOSConnectionParts(candidate.RouterOS.BaseURL)
		normalized, err := normalizedRouterOSURL(scheme, host, port)
		if err == nil && normalized == baseURL {
			return errors.New("another device already uses this RouterOS endpoint")
		}
	}
	return nil
}
