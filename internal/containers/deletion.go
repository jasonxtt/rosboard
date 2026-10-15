package containers

import (
	"context"
	"errors"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
	"strings"
	"unicode"
)

func (s *Service) checkRootRemoval(ctx context.Context, d config.DeviceConfig, guard map[string]string) error {
	root, err := DirectoryPath(guard["root-dir"])
	if err != nil || root == "/" || len(strings.Split(strings.TrimPrefix(root, "/"), "/")) < 2 {
		return operationError(409, "unsafe_root", "运行目录不能安全删除，操作已阻止")
	}
	overlap := func(value string) bool {
		p, e := DirectoryPath(value)
		return e == nil && p != "/" && (p == root || strings.HasPrefix(p, root+"/") || strings.HasPrefix(root, p+"/"))
	}
	rows, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerList)
	if err != nil {
		return err
	}
	own := findRows(rows, guard)
	if len(own) != 1 || !ownedComment(d, own[0]["comment"]) || containerStatus(own[0]) != "stopped" {
		return operationError(409, "root_in_use", "容器必须停止且归属未变更，才能删除运行目录")
	}
	for _, row := range rows {
		if row[".id"] != guard[".id"] && overlap(row["root-dir"]) {
			return operationError(409, "shared_root", "共享运行目录不能删除，操作已阻止")
		}
	}
	mounts, err := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerMounts)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if overlap(mount["src"]) {
			return operationError(409, "mounted_root", "运行目录与挂载数据重叠，操作已阻止")
		}
	}
	return nil
}

func nativeFailureMessage(err error, d config.DeviceConfig, r *nativeRecord) string {
	var response *routeros.HTTPError
	if !errors.As(err, &response) {
		return "设备操作失败；已完成的对象和数据已保留"
	}
	detail := response.Detail
	for _, secret := range []string{d.RouterOS.Username, d.RouterOS.Password, d.ContainerTransferCredentials().Username, d.ContainerTransferCredentials().Password} {
		if secret != "" {
			detail = strings.ReplaceAll(detail, secret, "[已隐藏]")
		}
	}
	for _, step := range r.Steps {
		if v := step.Fields["value"]; v != "" {
			detail = strings.ReplaceAll(detail, v, "[已隐藏]")
		}
	}
	detail = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, detail)
	runes := []rune(detail)
	if len(runes) > 400 {
		detail = string(runes[:400]) + "…"
	}
	if strings.TrimSpace(detail) == "" {
		return "设备拒绝操作；已完成的对象和数据已保留"
	}
	return "设备拒绝操作：" + detail + "。已完成的对象和数据已保留。"
}

func (s *Service) planRootRemoval(ctx context.Context, d config.DeviceConfig, guard map[string]string) (*nativeStep, error) {
	root, e := DirectoryPath(guard["root-dir"])
	if e != nil || root == "/" || len(strings.Split(strings.TrimPrefix(root, "/"), "/")) < 2 {
		return nil, operationError(409, "unsafe_root", "运行目录不能安全删除")
	}
	files, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerFiles)
	if e != nil {
		return nil, e
	}
	matches := findRows(files, map[string]string{"name": strings.TrimPrefix(root, "/")})
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) != 1 || (matches[0]["type"] != "directory" && matches[0]["type"] != "container store") {
		return nil, operationError(409, "unsafe_root", "运行目录类型无法确认，删除已阻止")
	}
	return &nativeStep{Menu: routeros.ContainerFiles, Command: "remove-root", Match: map[string]string{".id": matches[0][".id"], "name": matches[0]["name"], "type": matches[0]["type"]}, Guard: guard, Fields: map[string]string{}}, nil
}
func (s *Service) checkOrphanRootRemoval(ctx context.Context, d config.DeviceConfig, step nativeStep) error {
	if !ownedComment(d, step.Guard["comment"]) || !equivalent("root-dir", step.Match["name"], step.Guard["root-dir"]) {
		return operationError(409, "unsafe_root", "运行目录归属已变化，删除已阻止")
	}
	root, e := DirectoryPath(step.Match["name"])
	if e != nil || root == "/" || len(strings.Split(strings.TrimPrefix(root, "/"), "/")) < 2 {
		return operationError(409, "unsafe_root", "运行目录不能安全删除")
	}
	overlap := func(value string) bool {
		p, e := DirectoryPath(value)
		return e == nil && p != "/" && (p == root || strings.HasPrefix(p, root+"/") || strings.HasPrefix(root, p+"/"))
	}
	rows, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerList)
	if e != nil {
		return e
	}
	for _, row := range rows {
		if row[".id"] == step.Guard[".id"] || overlap(row["root-dir"]) {
			return operationError(409, "root_in_use", "运行目录仍被容器使用，删除已阻止")
		}
	}
	mounts, e := s.ReaderFor(d).ContainerRead(ctx, routeros.ContainerMounts)
	if e != nil {
		return e
	}
	for _, mount := range mounts {
		if overlap(mount["src"]) {
			return operationError(409, "mounted_root", "运行目录与挂载数据重叠，删除已阻止")
		}
	}
	return nil
}
