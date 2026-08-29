package policyv2

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"rosboard/internal/routeros"
)

func ValidateFakeAliases(ctx context.Context, reader PolicyReader, repository Repository) ([]PlanIssue, error) {
	egresses, err := repository.ListEgresses(ctx)
	if err != nil {
		return nil, err
	}
	actual := make([]netip.Prefix, 0)
	for _, menu := range []routeros.ReadMenu{routeros.ReadMenuIPAddress, routeros.ReadMenuIPv6Address} {
		objects, err := reader.PolicyList(ctx, menu, []string{"address", "disabled", "invalid"})
		if err != nil {
			return nil, fmt.Errorf("scan RouterOS addresses for Fake DNS collision: %w", err)
		}
		for _, object := range objects {
			if aliasRouterBool(object["disabled"]) || aliasRouterBool(object["invalid"]) {
				continue
			}
			value := strings.TrimSpace(object["address"])
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				if address, addressErr := netip.ParseAddr(value); addressErr == nil {
					bits := 128
					if address.Is4() {
						bits = 32
					}
					prefix = netip.PrefixFrom(address, bits)
				} else {
					continue
				}
			}
			actual = append(actual, prefix)
		}
	}
	issues := make([]PlanIssue, 0)
	sources, err := repository.ListSources(ctx, "")
	if err != nil {
		return nil, err
	}
	sourcesByEgress := enabledSourcesByEgress(sources)
	used := make(map[netip.Addr]string)
	for _, egress := range egresses {
		if egress.PendingDeletion || strings.TrimSpace(egress.FakeAlias) == "" {
			continue
		}
		// An IP-only egress materializes no DNS objects, so its persisted
		// alias must not block the plan.
		if !hasApplicableDomainSource(sourcesByEgress[egress.ID]) {
			continue
		}
		alias, err := netip.ParseAddr(strings.TrimSpace(egress.FakeAlias))
		if err != nil {
			issues = append(issues, issue("invalid_fake_alias", "", egress.ID, "Fake DNS 别名不是有效 IP"))
			continue
		}
		if otherID := used[alias]; otherID != "" {
			issues = append(issues, issue("fake_alias_conflict", "", egress.ID, "Fake DNS 别名与另一个出口重复"))
			continue
		}
		used[alias] = egress.ID
		for _, prefix := range actual {
			if prefix.Contains(alias) {
				issues = append(issues, issue("fake_alias_conflict", "", egress.ID, "Fake DNS 别名与 RouterOS 现有地址冲突"))
				break
			}
		}
	}
	return issues, nil
}

func aliasRouterBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}
