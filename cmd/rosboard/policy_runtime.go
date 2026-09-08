package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/accesscontrol"
	"rosboard/internal/config"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/service"
	"rosboard/internal/store"
)

// assemblePolicyRuntimes creates device-scoped appliers using the same
// RouterOS REST credentials as monitoring.
func assemblePolicyRuntimes(cfg config.Config, storage *store.Store, monitorManager *service.MonitorManager, manager *policyv2.Manager) error {
	for _, device := range cfg.Devices {
		if !device.Enabled || device.Archived || strings.TrimSpace(device.RouterOS.Username) == "" || device.RouterOS.Password == "" {
			continue
		}
		deviceStore, err := storage.OpenDevice(device.ID)
		if err != nil {
			return err
		}
		repo := deviceStore.PolicyRepository()
		reader := routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
		mutation := routeros.NewMutationClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
		applier := &policyv2.Applier{
			Mutation:  mutation,
			Reader:    reader,
			Repo:      repo,
			Access:    deviceStore.AccessRepository(),
			Terminals: accessTerminalResolver(monitorManager, device.ID),
			Scope:     accessScopeResolver(monitorManager, device.ID),
			Refresh:   policySourceRefresher(policy.NewSourceFetcher(policy.FetcherOptions{}), device.ID),
		}
		if err := manager.RegisterApplier(device.ID, applier); err != nil {
			return err
		}
	}
	return nil
}

func accessTerminalResolver(manager *service.MonitorManager, deviceID string) policyv2.TerminalResolver {
	if manager == nil {
		return nil
	}
	return func() []accesscontrol.Terminal {
		monitor, err := manager.Monitor(deviceID)
		if err != nil {
			return nil
		}
		snapshot := monitor.Snapshot()
		result := make([]accesscontrol.Terminal, 0, len(snapshot.Terminals))
		for _, terminal := range snapshot.Terminals {
			result = append(result, accesscontrol.Terminal{
				ID: terminal.ID, DisplayName: terminal.DisplayName, MACAddress: terminal.MACAddress,
				IPv4: append([]string(nil), terminal.IPv4...), IPv6: append([]string(nil), terminal.IPv6...),
			})
		}
		return result
	}
}

// accessScopeResolver exposes monitor local-network evidence to the
// access-control desired builder. Internet-scope rules resolve their egress
// interfaces from RouterOS routes; this evidence only excludes local
// interfaces from that route projection.
func accessScopeResolver(manager *service.MonitorManager, deviceID string) policyv2.ScopeResolver {
	if manager == nil {
		return nil
	}
	return func() accesscontrol.Scope {
		monitor, err := manager.Monitor(deviceID)
		if err != nil {
			return accesscontrol.Scope{}
		}
		snapshot := monitor.Snapshot()
		prefixes := make([]accesscontrol.ScopePrefix, 0, len(snapshot.TerminalScope.Prefixes))
		localInterfaces := make([]string, 0, len(snapshot.TerminalScope.Interfaces)+len(snapshot.TerminalScope.Prefixes))
		for _, prefix := range snapshot.TerminalScope.Prefixes {
			if strings.TrimSpace(prefix.CIDR) == "" {
				continue
			}
			family := strings.ToLower(strings.TrimSpace(prefix.Family))
			if family == "" {
				parsed, parseErr := netip.ParsePrefix(strings.TrimSpace(prefix.CIDR))
				if parseErr != nil {
					continue
				}
				family = accesscontrol.FamilyIPv4
				if parsed.Addr().Is6() {
					family = accesscontrol.FamilyIPv6
				}
			}
			if family != accesscontrol.FamilyIPv4 && family != accesscontrol.FamilyIPv6 {
				continue
			}
			prefixes = append(prefixes, accesscontrol.ScopePrefix{CIDR: prefix.CIDR, Family: family, Interface: prefix.Interface})
			if strings.TrimSpace(prefix.Interface) != "" {
				localInterfaces = append(localInterfaces, prefix.Interface)
			}
		}
		for _, iface := range snapshot.TerminalScope.Interfaces {
			if strings.EqualFold(strings.TrimSpace(iface.Role), "lan") && strings.TrimSpace(iface.Name) != "" {
				localInterfaces = append(localInterfaces, iface.Name)
			}
		}
		return accesscontrol.Scope{Prefixes: prefixes, LocalInterfaces: localInterfaces}
	}
}

func policySourceRefresher(fetcher *policy.SourceFetcher, deviceID string) policyv2.SourceRefresher {
	return func(ctx context.Context, source policyv2.Source) (policyv2.SourceRefresh, error) {
		preview, err := fetcher.Preview(ctx, source.URL, policy.FetchOptions{ETag: source.ETag, LastModified: source.LastModified, Kind: source.Kind})
		if err != nil {
			return policyv2.SourceRefresh{}, err
		}
		result := policyv2.SourceRefresh{NotModified: preview.NotModified, ETag: preview.ETag, LastModified: preview.LastModified}
		if preview.NotModified {
			return result, nil
		}
		legacyVersion, legacyRules, err := preview.PendingVersion(deviceID, source.ID, uuid.NewString())
		if err != nil {
			return policyv2.SourceRefresh{}, err
		}
		counts := map[string]int{}
		diff := map[string]any{}
		_ = json.Unmarshal(legacyVersion.CountsJSON, &counts)
		_ = json.Unmarshal(legacyVersion.DiffSummaryJSON, &diff)
		version := policyv2.SourceVersion{
			ID: legacyVersion.ID, SourceID: source.ID, SHA256: legacyVersion.SHA256,
			CompressedYAML: append([]byte(nil), legacyVersion.CompressedYAML...), State: "pending",
			HTTPStatus: preview.StatusCode, Counts: counts, Diff: diff, CreatedAt: time.Now().UTC(),
		}
		rules := make([]policyv2.SourceRule, len(legacyRules))
		for index, rule := range legacyRules {
			rules[index] = policyv2.SourceRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
		}
		result.Version, result.Rules = &version, rules
		return result, nil
	}
}
