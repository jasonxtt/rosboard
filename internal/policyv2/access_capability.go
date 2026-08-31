package policyv2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/routeros"
)

func accessCapabilityBlockers(ctx context.Context, mutation PolicyMutation, desired []DesiredObject) ([]PlanIssue, error) {
	menus := make(map[routeros.MutationMenu]bool)
	for _, object := range desired {
		if !strings.HasPrefix(object.LogicalID, "access:") || object.Fields["chain"] == "" {
			continue
		}
		menu := routeros.MutationMenu(object.Menu)
		if menu == routeros.MenuIPFirewallFilter || menu == routeros.MenuIPv6FirewallFilter {
			menus[menu] = true
		}
	}
	ordered := make([]routeros.MutationMenu, 0, len(menus))
	for menu := range menus {
		ordered = append(ordered, menu)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	if len(ordered) == 0 {
		return nil, nil
	}

	verifier, ok := mutation.(AccessCapabilityVerifier)
	if !ok {
		return accessCapabilityIssues(ordered, errors.New("RouterOS mutation client does not implement the access-control capability probe")), nil
	}
	if err := verifier.VerifyAccessControlCapabilities(ctx, ordered); err != nil {
		return accessCapabilityIssues(ordered, err), nil
	}
	return nil, nil
}

func accessCapabilityIssues(menus []routeros.MutationMenu, err error) []PlanIssue {
	issues := make([]PlanIssue, 0, len(menus))
	for _, menu := range menus {
		family := string(FamilyIPv4)
		if menu == routeros.MenuIPv6FirewallFilter {
			family = string(FamilyIPv6)
		}
		issues = append(issues, PlanIssue{
			Code: "routeros_access_filter_capability_unverified", Status: "blocker", Family: family,
			Reason: fmt.Sprintf("无法证明 RouterOS %s 过滤器支持访问控制所需的 address-list、jump/return、drop 及 reject-with=tcp-reset 能力：%v", family, err),
		})
	}
	return issues
}
