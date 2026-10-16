package containers

import (
	"context"
	"maps"
	"strings"

	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

type retainedUpdate struct {
	before, after map[string]string
	root          *nativeStep
	invalid       bool
}

func rootIdentity(guard map[string]string) string {
	return strings.Join([]string{guard[".id"], strings.TrimPrefix(guard["root-dir"], "/"), guard["interface"], guard["comment"]}, "\x00")
}

// Follow completed cutovers by exact input/output identity, not by reusable ROS
// IDs or a device-wide ownership prefix. Names may have been edited afterward.
func (s *Service) retainedUpdates(d config.DeviceConfig) ([]retainedUpdate, error) {
	n, err := s.nativeDevice(d)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var updates []retainedUpdate
	for _, record := range n.state.Jobs {
		if record.Request.Action != "update" || record.Job.Action != "update" || record.Job.DeviceID != d.ID {
			continue
		}
		var add, remove, cutover, retained *nativeStep
		complete := true
		for i := range record.Steps {
			step := &record.Steps[i]
			if !step.Done {
				complete = false
			}
			if step.Menu == routeros.ContainerList {
				switch step.Command {
				case "add":
					add = step
				case "remove":
					remove = step
				case "set":
					cutover = step
				}
			} else if step.Menu == routeros.ContainerFiles && step.Command == "retain-root" {
				retained = step
			}
		}
		if !complete || add == nil || remove == nil || cutover == nil || record.Job.TargetID == "" {
			continue
		}
		before := remove.Match
		marker := managedComment(d, record.Job.ID)
		if !ownedComment(d, before["comment"]) || before[".id"] == "" || before["root-dir"] == "" ||
			record.Request.TargetID != before[".id"] || add.Fields["comment"] != marker ||
			add.Fields["root-dir"] == "" || add.Fields["interface"] != before["interface"] ||
			cutover.Match["comment"] != marker || cutover.Match["root-dir"] != add.Fields["root-dir"] ||
			cutover.Match["interface"] != before["interface"] || cutover.Fields["comment"] != before["comment"] {
			continue
		}
		invalid := false
		if retained != nil {
			if len(record.Job.ID) < 8 || rootIdentity(retained.Guard) != rootIdentity(before) ||
				!equivalent("root-dir", retained.Match["name"], before["root-dir"]) ||
				retained.Fields["name"] != strings.TrimPrefix(before["root-dir"], "/")+"-retained-"+record.Job.ID[:8] {
				invalid = true
			}
			copy := *retained
			copy.Match, copy.Fields, copy.Guard = maps.Clone(retained.Match), maps.Clone(retained.Fields), maps.Clone(retained.Guard)
			retained = &copy
		}
		updates = append(updates, retainedUpdate{before: maps.Clone(before), after: map[string]string{
			".id": record.Job.TargetID, "root-dir": add.Fields["root-dir"],
			"interface": add.Fields["interface"], "comment": cutover.Fields["comment"],
		}, root: retained, invalid: invalid})
	}
	return updates, nil
}

func (s *Service) planRetainedRootRemoval(ctx context.Context, d config.DeviceConfig, guard map[string]string) ([]nativeStep, error) {
	updates, err := s.retainedUpdates(d)
	if err != nil {
		return nil, err
	}
	var history []nativeStep
	visited := map[string]bool{}
	for {
		identity := rootIdentity(guard)
		if visited[identity] {
			return nil, operationError(409, "historical_root_unbound", "历史运行目录归属记录存在循环，删除已阻止")
		}
		visited[identity] = true
		var found *retainedUpdate
		for i := range updates {
			if rootIdentity(updates[i].after) == identity {
				if found != nil {
					return nil, operationError(409, "historical_root_unbound", "历史运行目录归属记录不唯一，删除已阻止")
				}
				found = &updates[i]
			}
		}
		if found == nil {
			break
		}
		if found.invalid {
			return nil, operationError(409, "historical_root_unbound", "历史运行目录归属记录不完整，删除已阻止")
		}
		if found.root != nil {
			history = append(history, *found.root)
		}
		guard = found.before
	}
	if len(history) == 0 {
		return nil, nil
	}
	files, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if err != nil {
		return nil, err
	}
	var steps []nativeStep
	for _, retained := range history {
		name := retained.Fields["name"]
		matches := findRows(files, map[string]string{"name": name})
		if len(matches) == 0 {
			continue
		}
		// Legacy journals are usable only if the exact original Files ID survived
		// rename. Never bind an arbitrary current object just by its old pathname.
		id := retained.ResultID
		if id == "" {
			id = retained.Match[".id"]
		}
		if id == "" || len(matches) != 1 || matches[0][".id"] != id || matches[0]["type"] != retained.Match["type"] ||
			(matches[0]["type"] != "container store" && matches[0]["type"] != "directory") {
			return nil, operationError(409, "historical_root_changed", "历史运行目录身份无法确认，请先在文件管理中确认处理："+name)
		}
		step := nativeStep{Menu: routeros.ContainerFiles, Command: "remove-root",
			Match: map[string]string{".id": id, "name": name, "type": matches[0]["type"]},
			Guard: map[string]string{"comment": retained.Guard["comment"], "root-dir": name}, Fields: map[string]string{}}
		if err := s.checkOrphanRootRemoval(ctx, d, step); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}
