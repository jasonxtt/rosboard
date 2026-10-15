package api

import (
	"context"
	"errors"
	"rosboard/internal/config"
	"rosboard/internal/routeros"
)

// First-use trust is scoped to an explicit authenticated transfer operation.
// Ordinary GET/preview/capability reads never persist connection authority.
func (s *Server) automaticTransfer(ctx context.Context, d config.DeviceConfig) (config.DeviceConfig, error) {
	if !d.Enabled || d.Archived {
		return d, errors.New("设备不存在或未启用")
	}
	if d.ContainerTransferCredentials().HostKey != "" {
		return d, nil
	}
	if _, err := routeros.NewClient(d.RouterOS.BaseURL, d.RouterOS.Username, d.RouterOS.Password).SystemResource(ctx); err != nil {
		return d, errors.New("设备连接校验失败，请检查连接账号")
	}
	candidate, err := verifyAutomaticTransfer(ctx, d)
	if err != nil {
		return d, errors.New("镜像传输连接失败，请检查设备 SSH 服务、端口及账号权限")
	}
	return s.persistAutomaticTransfer(d, candidate)
}
func verifyAutomaticTransfer(ctx context.Context, d config.DeviceConfig) (config.DeviceConfig, error) {
	c := d.ContainerTransferCredentials()
	key, err := routeros.DiscoverContainerTransferKey(ctx, d.RouterOS.BaseURL, c.Port)
	if err != nil {
		return d, err
	}
	if err = routeros.CheckContainerTransfer(ctx, d.RouterOS.BaseURL, c.Username, c.Password, key, c.Port); err != nil {
		return d, err
	}
	if d.ContainerTransfer != nil {
		copy := *d.ContainerTransfer
		copy.HostKey = key
		d.ContainerTransfer = &copy
	} else {
		d.ContainerSSHHostKey = key
	}
	return d, nil
}
func (s *Server) persistAutomaticTransfer(before, verified config.DeviceConfig) (config.DeviceConfig, error) {
	s.deviceSaveMu.Lock()
	defer s.deviceSaveMu.Unlock()
	current, ok := s.configSnapshot().Device(before.ID)
	if !ok || !current.Enabled || current.Archived || transferSettingsRevision(current) != transferSettingsRevision(before) || before.ContainerTransferCredentials().HostKey != "" {
		return before, errors.New("设备连接已变化，请重试操作")
	}
	unpinned := verified
	if verified.ContainerTransfer != nil {
		c := *verified.ContainerTransfer
		c.HostKey = before.ContainerTransferCredentials().HostKey
		unpinned.ContainerTransfer = &c
	} else {
		unpinned.ContainerSSHHostKey = before.ContainerSSHHostKey
	}
	if verified.ID != before.ID || transferSettingsRevision(unpinned) != transferSettingsRevision(before) {
		return before, errors.New("设备传输身份已变化，请重试操作")
	}
	if verified.ContainerTransferCredentials().HostKey == "" {
		return before, errors.New("镜像传输连接未确认")
	}
	err := s.saveSettings(func(next *config.Config) {
		for i := range next.Devices {
			if next.Devices[i].ID == before.ID {
				next.Devices[i].ContainerSSHHostKey = verified.ContainerSSHHostKey
				next.Devices[i].ContainerTransfer = verified.ContainerTransfer
			}
		}
	})
	if err != nil {
		return before, errors.New("无法保存镜像传输连接")
	}
	return verified, nil
}
