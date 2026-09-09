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
	scheduledMenus := make(map[routeros.MutationMenu]bool)
	for _, object := range desired {
		if !strings.HasPrefix(object.LogicalID, "access:") || object.Fields["chain"] == "" {
			continue
		}
		menu := routeros.MutationMenu(object.Menu)
		if menu == routeros.MenuIPFirewallFilter || menu == routeros.MenuIPv6FirewallFilter {
			menus[menu] = true
			if strings.TrimSpace(object.Fields["time"]) != "" {
				scheduledMenus[menu] = true
			}
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
	if len(scheduledMenus) > 0 {
		timeVerifier, ok := mutation.(AccessTimeCapabilityVerifier)
		if !ok {
			return accessTimeCapabilityIssues(ordered, errors.New("RouterOS mutation client does not implement the access-control time capability probe")), nil
		}
		if err := timeVerifier.VerifyAccessControlTimeCapabilities(ctx, ordered); err != nil {
			return accessTimeCapabilityIssues(ordered, err), nil
		}
	}
	if scheduledMenus[routeros.MenuIPFirewallFilter] {
		issues, err := scheduledAccessFastTrackBlockers(ctx, mutation)
		if err != nil {
			return nil, err
		}
		if len(issues) > 0 {
			return issues, nil
		}
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

func accessTimeCapabilityIssues(menus []routeros.MutationMenu, err error) []PlanIssue {
	issues := make([]PlanIssue, 0, len(menus))
	for _, menu := range menus {
		family := string(FamilyIPv4)
		if menu == routeros.MenuIPv6FirewallFilter {
			family = string(FamilyIPv6)
		}
		issues = append(issues, PlanIssue{
			Code: "routeros_access_time_capability_unverified", Status: "blocker", Family: family,
			Reason: fmt.Sprintf("无法证明 RouterOS %s 过滤器支持访问控制的 time 时间匹配器；限时规则已阻止同步：%v", family, err),
		})
	}
	return issues
}

const scheduledAccessFastTrackCode = "routeros_access_scheduled_fasttrack_unverified"

func scheduledAccessFastTrackBlockers(ctx context.Context, mutation PolicyMutation) ([]PlanIssue, error) {
	objects, err := mutation.List(ctx, routeros.MenuIPFirewallFilter, routeros.MutationQuery{
		Proplist: []string{".id", "action", "disabled"},
	})
	if err != nil {
		return []PlanIssue{scheduledAccessFastTrackIssue(fmt.Errorf("无法读取 IPv4 firewall filter：%w", err))}, nil
	}
	for _, object := range objects {
		if !strings.EqualFold(strings.TrimSpace(object["action"]), "fasttrack-connection") {
			continue
		}
		disabled, err := object.Bool("disabled")
		if err != nil {
			return []PlanIssue{scheduledAccessFastTrackIssue(fmt.Errorf("FastTrack 规则 %q 的 disabled 状态无效：%w", object.ID(), err))}, nil
		}
		if !disabled {
			return []PlanIssue{scheduledAccessFastTrackIssue(fmt.Errorf("发现启用中的 FastTrack 规则 %q", object.ID()))}, nil
		}
	}
	return nil, nil
}

func scheduledAccessFastTrackIssue(err error) PlanIssue {
	return PlanIssue{
		Code: scheduledAccessFastTrackCode, Status: "blocker", Family: string(FamilyIPv4),
		Reason: fmt.Sprintf("无法证明限时访问规则会在窗口开始后重新检查已有 IPv4 连接；请停用或排除 FastTrack 后再应用：%v", err),
	}
}
