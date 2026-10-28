package policyv2

import (
	"context"
	"fmt"
	"strings"

	"rosboard/internal/routeros"
)

// A connection mark cannot classify untracked packets. Refuse application
// when a raw notrack rule could bypass the policy; never mutate foreign raw rules.
func routingBlockTrackingIssues(ctx context.Context, reader PolicyReader, desired []DesiredObject) ([]PlanIssue, error) {
	menus := map[routeros.ReadMenu]bool{}
	for _, o := range desired {
		if !isRoutingBlockFilter(o) || normalizeRouterValue(o.Fields["disabled"]) == "yes" {
			continue
		}
		if o.Menu == string(routeros.MenuIPFirewallFilter) {
			menus[routeros.ReadMenuIPFirewallRaw] = true
		}
		if o.Menu == string(routeros.MenuIPv6FirewallFilter) {
			menus[routeros.ReadMenuIPv6FirewallRaw] = true
		}
	}
	if len(menus) == 0 {
		return nil, nil
	}
	tracking, err := reader.PolicyList(ctx, routeros.ReadMenuFirewallTracking, []string{"enabled"})
	if err != nil {
		return nil, fmt.Errorf("read connection tracking: %w", err)
	}
	if len(tracking) != 1 || strings.TrimSpace(tracking[0]["enabled"]) == "" {
		return []PlanIssue{{Code: "routing_block_tracking_unknown", Status: "blocker", Reason: "无法确认连接跟踪状态；关闭地址族的阻断无法安全应用。"}}, nil
	}
	trackingMode := normalizeRouterValue(tracking[0]["enabled"])
	if trackingMode != "yes" && trackingMode != "no" && trackingMode != "auto" {
		return []PlanIssue{{Code: "routing_block_tracking_unknown", Status: "blocker", Reason: "无法确认连接跟踪状态；关闭地址族的阻断无法安全应用。"}}, nil
	}
	if trackingMode == "no" {
		return []PlanIssue{{Code: "routing_block_tracking_disabled", Status: "blocker", Reason: "连接跟踪已关闭，无法安全识别需要阻断的新连接。"}}, nil
	}
	var issues []PlanIssue
	for _, menu := range []routeros.ReadMenu{routeros.ReadMenuIPFirewallRaw, routeros.ReadMenuIPv6FirewallRaw} {
		if !menus[menu] {
			continue
		}
		rules, err := reader.PolicyList(ctx, menu, []string{".id", "action", "disabled"})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", menu, err)
		}
		for _, rule := range rules {
			if strings.EqualFold(rule["action"], "notrack") && normalizeRouterValue(rule["disabled"]) != "yes" {
				issues = append(issues, PlanIssue{Code: "routing_block_notrack", Status: "blocker", LogicalID: rule.ID(), Reason: fmt.Sprintf("%s 存在启用的 notrack 规则 %s，可能绕过地址族阻断；请先排除冲突并重新预览。", menu, rule.ID())})
			}
		}
	}
	return issues, nil
}

// Conservatively flag foreign classifiers that could mark or terminate a
// packet before our ordered winner rules. Ownership comes from the scanner,
// never from a name or prefix supplied by another manager.
func routingBlockMangleIssues(ctx context.Context, mutation PolicyMutation, desired []DesiredObject, actual []ActualObject) ([]PlanIssue, error) {
	chains := map[routeros.MutationMenu]map[string]bool{}
	for _, o := range desired {
		if !isRoutingBlockFilter(o) || normalizeRouterValue(o.Fields["disabled"]) == "yes" {
			continue
		}
		menu := routeros.MenuIPFirewallMangle
		if o.Menu == string(routeros.MenuIPv6FirewallFilter) {
			menu = routeros.MenuIPv6FirewallMangle
		}
		if chains[menu] == nil {
			chains[menu] = map[string]bool{}
		}
		if o.Fields["chain"] == "forward" {
			chains[menu]["prerouting"] = true
			chains[menu]["forward"] = true
		}
		if o.Fields["chain"] == "output" {
			chains[menu]["output"] = true
		}
	}
	owned := map[string]bool{}
	for _, o := range actual {
		if o.Ownership == "owned" {
			owned[o.Menu+"\x00"+o.RouterID] = true
		}
	}
	var issues []PlanIssue
	for _, menu := range []routeros.MutationMenu{routeros.MenuIPFirewallMangle, routeros.MenuIPv6FirewallMangle} {
		if len(chains[menu]) == 0 {
			continue
		}
		rules, err := mutation.List(ctx, menu, routeros.MutationQuery{})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", menu, err)
		}
		for _, r := range rules {
			chain := strings.ToLower(strings.TrimSpace(r["chain"]))
			if !chains[menu][chain] || normalizeRouterValue(r["disabled"]) == "yes" {
				continue
			}
			action := strings.ToLower(strings.TrimSpace(r["action"]))
			if chain == "forward" {
				// Forward mangle executes after classification but before filter.
				// A jump can reach an arbitrary mark writer; neither it nor a
				// direct mark-connection is safe to assume disjoint. Other
				// forward actions cannot overwrite the connection mark.
				if action != "mark-connection" && action != "jump" {
					continue
				}
			} else {
				if owned[string(menu)+"\x00"+r.ID()] {
					continue
				}
				// These actions continue without changing connection classification.
				if action == "log" || action == "passthrough" || (action == "change-mss" && normalizeRouterValue(r["passthrough"]) != "no") {
					continue
				}
			}
			issues = append(issues, PlanIssue{Code: "routing_block_mangle_conflict", Status: "warning", LogicalID: r.ID(), Reason: fmt.Sprintf("%s 的 %s 链规则 %s 可能抢先分类或覆盖策略连接标记；请确认其匹配范围与策略流量不重叠。", menu, chain, r.ID())})
		}
	}
	return issues, nil
}
