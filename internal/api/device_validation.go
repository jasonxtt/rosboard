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
			TrafficInterfaces: interfaces, TerminalCIDRs: cidrs,
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
	if len(result) == 0 {
		return nil, errors.New("at least one local CIDR is required")
	}
	sort.Strings(result)
	return result, nil
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
