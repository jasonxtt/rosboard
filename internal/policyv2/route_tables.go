package policyv2

import (
	"context"
	"strings"

	"rosboard/internal/routeros"
)

func ValidateRouteTables(ctx context.Context, reader PolicyReader, repository Repository) ([]PlanIssue, []PlanIssue, error) {
	objects, err := reader.PolicyList(ctx, routeros.ReadMenuRoutingTable, []string{"name", "fib", "invalid"})
	if err != nil {
		return nil, nil, err
	}
	existing := make(map[string]routeros.RouterOSObject, len(objects))
	for _, object := range objects {
		existing[strings.TrimSpace(object["name"])] = object
	}
	managerID, err := repository.ManagerInstanceID(ctx)
	if err != nil {
		return nil, nil, err
	}
	egresses, err := repository.ListEgresses(ctx)
	if err != nil {
		return nil, nil, err
	}
	blockers := make([]PlanIssue, 0)
	warnings := make([]PlanIssue, 0)
	for _, egress := range egresses {
		if egress.PendingDeletion {
			continue
		}
		for _, family := range egress.Families {
			if !family.Enabled {
				continue
			}
			table := strings.TrimSpace(family.RouteTable)
			auto := DefaultRouteTable(managerID, repository.DeviceID(), egress.ID, family.Family)
			if table == "" || table == auto {
				continue
			}
			if strings.EqualFold(table, "main") {
				warnings = append(warnings, PlanIssue{Code: "main_table_reuse", Status: "warning", Family: string(family.Family), EgressID: egress.ID, Reason: "复用 RouterOS main 路由表，其现有故障切换逻辑不由 rosboard 管理"})
				continue
			}
			object, ok := existing[table]
			if !ok || aliasRouterBool(object["invalid"]) {
				blockers = append(blockers, issue("route_table_not_found", string(family.Family), egress.ID, "自定义路由表不存在；留空可由 rosboard 自动创建"))
				continue
			}
			warnings = append(warnings, PlanIssue{Code: "custom_route_table_reuse", Status: "warning", Family: string(family.Family), EgressID: egress.ID, Reason: "复用现有自定义路由表，rosboard 不修改或删除该表"})
		}
	}
	return blockers, warnings, nil
}
