package policyv2

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rosboard/internal/ownership"
	"rosboard/internal/routeros"
)

// FastTrack is foreign configuration. It never enters DesiredObject ownership.
// Snapshots are optimistic checks, not an atomic RouterOS compare-and-swap.
type FastTrackRecord struct {
	Menu    routeros.MutationMenu `json:"menu"`
	ID      string                `json:"id"`
	Before  map[string]string     `json:"before"`
	Applied map[string]string     `json:"applied"`
	Status  string                `json:"status"`
}
type FastTrackState struct {
	JobID       string            `json:"jobId,omitempty"`
	JobWarnings []PlanIssue       `json:"jobWarnings,omitempty"`
	Records     []FastTrackRecord `json:"records"`
	Events      []PlanIssue       `json:"events"`
}
type FastTrackRepository interface {
	LoadFastTrackState(context.Context) (FastTrackState, error)
	SaveFastTrackState(context.Context, FastTrackState) error
}
type fastTrackUnset interface {
	UnsetFirewallConnectionMark(context.Context, routeros.MutationMenu, string) error
}
type FastTrackRuleAnalysis struct {
	Menu   routeros.MutationMenu `json:"menu"`
	ID     string                `json:"id"`
	Status string                `json:"status"`
	Reason string                `json:"reason"`
	Before map[string]string     `json:"-"`
}
type FastTrackReport struct {
	RetainOnly  bool                    `json:"retainOnly,omitempty"`
	Fingerprint string                  `json:"fingerprint"`
	Consumers   int                     `json:"consumers"`
	Rules       []FastTrackRuleAnalysis `json:"rules"`
	Events      []PlanIssue             `json:"events,omitempty"`
}

func fastTrackFields(fields map[string]string) map[string]string {
	result := cloneStrings(fields)
	for _, key := range []string{".id", "bytes", "packets", "invalid", "dynamic", "hw-offloaded", "last-used", "last-seen"} {
		delete(result, key)
	}
	// RouterOS uses both CLI and REST boolean spellings. Absent disabled is false.
	for _, key := range []string{"disabled", "hw-offload", "log"} {
		if value, ok := result[key]; ok {
			result[key] = normalizeRouterValue(value)
		}
	}
	if result["disabled"] == "" {
		result["disabled"] = "no"
	}
	return result
}
func fastTrackHash(value any) string {
	data, _ := json.Marshal(value)
	return shortHash(string(data), 64)
}
func fastTrackKey(menu routeros.MutationMenu, id string) string { return string(menu) + ":" + id }
func fastTrackConsumers(ctx context.Context, repo Repository) (int, error) {
	r, ok := repo.(RoutingRuleRepository)
	if !ok {
		return 0, nil
	}
	rules, err := r.ListRoutingRules(ctx)
	return len(rules), err // Disabled rules still hold compatibility.
}

func inspectFastTrack(ctx context.Context, applier *Applier, repo Repository, desired []DesiredObject) (*FastTrackReport, error) {
	count, err := fastTrackConsumers(ctx, repo)
	if err != nil {
		return nil, err
	}
	state := FastTrackState{}
	store, durable := applier.Repo.(FastTrackRepository)
	if durable {
		state, err = store.LoadFastTrackState(ctx)
		if err != nil {
			return nil, err
		}
	}
	if count == 0 && len(state.Records) == 0 && len(state.Events) == 0 {
		return nil, nil
	}
	report := &FastTrackReport{Consumers: count, Rules: []FastTrackRuleAnalysis{}, Events: state.Events}
	marks := map[string]bool{}
	managerID, err := applier.Repo.ManagerInstanceID(ctx)
	if err != nil {
		return nil, err
	}
	ownedComments := map[string]bool{}
	for _, obj := range desired {
		if obj.Fields["action"] == "mark-connection" {
			marks[obj.Fields["new-connection-mark"]] = true
			ownedComments[managedCommentIdentity(obj.Fields["comment"])] = true
		}
	}
	records := map[string]FastTrackRecord{}
	for _, record := range state.Records {
		records[fastTrackKey(record.Menu, record.ID)] = record
	}
	dependencies := map[string]any{"records": state.Records, "consumers": count, "marks": marks}
	for _, pair := range [][2]routeros.MutationMenu{{routeros.MenuIPFirewallFilter, routeros.MenuIPFirewallMangle}, {routeros.MenuIPv6FirewallFilter, routeros.MenuIPv6FirewallMangle}} {
		filters, err := applier.Mutation.List(ctx, pair[0], routeros.MutationQuery{})
		if err != nil {
			return nil, err
		}
		mangle, err := applier.Mutation.List(ctx, pair[1], routeros.MutationQuery{})
		if err != nil {
			return nil, err
		}
		foreign := false
		producers := map[string]map[string]string{}
		for _, obj := range mangle {
			if obj["action"] != "mark-connection" || normalizeRouterValue(obj["disabled"]) == "yes" {
				continue
			}
			if ownedComments[managedCommentIdentity(obj["comment"])] || ownership.IsCanonicalFor(managerID, applier.Repo.DeviceID(), obj["comment"]) {
				continue
			}
			foreign = true
			producers[obj.ID()] = fastTrackFields(obj)
		}
		dependencies[string(pair[1])] = producers
		snapshots := map[string]map[string]string{}
		for _, obj := range filters {
			// Include all filter configuration: custom-chain reachability and changes of action/disabled invalidate preview.
			fields := fastTrackFields(obj)
			snapshots[obj.ID()] = fields
			if obj["action"] != "fasttrack-connection" || fields["disabled"] == "yes" {
				continue
			}
			analysis := FastTrackRuleAnalysis{Menu: pair[0], ID: obj.ID(), Before: fields, Status: "unverified", Reason: "自定义 FastTrack 无法可靠判断，请确认已排除策略连接；继续后策略路由可能不稳定。"}
			mark := obj["connection-mark"]
			switch {
			case mark == "no-mark" || (mark != "" && !strings.ContainsAny(mark, "!,*? ") && !marks[mark]):
				analysis.Status = "compatible"
				analysis.Reason = "FastTrack 已排除当前策略连接，无需修改。"
			case simpleFastTrack(fields) && !foreign:
				analysis.Status = "auto_fixable"
				analysis.Reason = "将添加 connection-mark=no-mark，使策略连接不进入 FastTrack。"
			case simpleFastTrack(fields) && foreign:
				analysis.Status = "conflict"
				analysis.Reason = "FastTrack 未排除策略连接，但自动收窄可能影响其他连接标记；请确认风险后继续。"
			}
			if record, ok := records[fastTrackKey(pair[0], obj.ID())]; ok {
				diverged := record.Status == "diverged" || (fastTrackHash(fields) != fastTrackHash(record.Applied) && !(record.Status == "intent" && fastTrackHash(fields) == fastTrackHash(record.Before)))
				if diverged && analysis.Status == "auto_fixable" {
					analysis.Status = "unverified"
					analysis.Reason = "曾调整的 FastTrack 已发生外部修改，保留当前配置；请确认策略连接已被排除。"
				}
			}
			if normalizeRouterValue(obj["dynamic"]) == "yes" {
				analysis.Status = "unverified"
				analysis.Reason = "动态 FastTrack 无法安全自动修改，请确认已排除策略连接。"
			}
			if analysis.Status == "auto_fixable" {
				_, canUnset := applier.Mutation.(fastTrackUnset)
				if !durable || !canUnset {
					analysis.Status = "unverified"
					analysis.Reason = "当前设备适配器不支持持久化或精确恢复 FastTrack，未自动修改；请确认风险后继续。"
				}
			}
			report.Rules = append(report.Rules, analysis)
		}
		dependencies[string(pair[0])] = snapshots
	}
	sort.Slice(report.Rules, func(i, j int) bool {
		return fastTrackKey(report.Rules[i].Menu, report.Rules[i].ID) < fastTrackKey(report.Rules[j].Menu, report.Rules[j].ID)
	})
	report.Fingerprint = fastTrackHash(dependencies)
	return report, nil
}
func simpleFastTrack(fields map[string]string) bool {
	if fields["chain"] != "forward" || fields["connection-mark"] != "" {
		return false
	}
	state := fields["connection-state"]
	if state != "established,related" && state != "related,established" {
		return false
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		switch key {
		case "chain", "action", "disabled", "connection-state", "connection-mark", "comment", "hw-offload", "log", "log-prefix":
		default:
			return false
		}
	}
	return true
}
func addFastTrackPlan(plan *Plan, report *FastTrackReport) {
	plan.FastTrack = report
	if report == nil {
		return
	}
	for _, rule := range report.Rules {
		if report.Consumers == 0 || report.RetainOnly {
			continue
		}
		code := ""
		switch rule.Status {
		case "auto_fixable":
			code = "fasttrack_auto_adjustment_planned"
		case "conflict":
			code = "fasttrack_policy_routing_conflict"
		case "unverified":
			code = "fasttrack_compatibility_unverified"
		}
		if code == "" {
			continue
		}
		plan.Warnings = append(plan.Warnings, PlanIssue{Code: code, LogicalID: fastTrackKey(rule.Menu, rule.ID), Reason: fmt.Sprintf("FastTrack %s: %s", rule.ID, rule.Reason)})
		if rule.Status != "auto_fixable" {
			found := false
			for _, ack := range plan.Acknowledgements {
				if ack.Code == code {
					found = true
				}
			}
			if !found {
				plan.Acknowledgements = append(plan.Acknowledgements, PlanAcknowledgement{Code: code, Required: true})
			}
		}
	}
	plan.Warnings = append(plan.Warnings, report.Events...)
}
func checkFastTrackPlan(ctx context.Context, applier *Applier, repo Repository, desired []DesiredObject, plan Plan) error {
	if plan.Domain == PolicyDomainAccess {
		return nil
	}
	report, err := inspectFastTrack(ctx, applier, repo, desired)
	if err != nil {
		return err
	}
	if (report == nil) != (plan.FastTrack == nil) {
		return ErrPlanStale
	}
	if report != nil && report.Fingerprint != plan.FastTrack.Fingerprint {
		return ErrPlanStale
	}
	return nil
}
