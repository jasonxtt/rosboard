package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

type ContainerMenu string

const (
	ContainerList       ContainerMenu = "container"
	ContainerConfig     ContainerMenu = "container/config"
	ContainerVETH       ContainerMenu = "interface/veth"
	ContainerBridge     ContainerMenu = "interface/bridge"
	ContainerBridgePort ContainerMenu = "interface/bridge/port"
	ContainerInterfaces ContainerMenu = "interface"
	ContainerAddresses  ContainerMenu = "ip/address"
	ContainerDisk       ContainerMenu = "disk"
	ContainerEnvs       ContainerMenu = "container/envs"
	ContainerMounts     ContainerMenu = "container/mounts"
	ContainerResource   ContainerMenu = "system/resource"
	ContainerLogs       ContainerMenu = "container/log"
	ContainerFiles      ContainerMenu = "file"
)

// ContainerRead is a closed GET-only reader. Credentials and config-json are
// excluded even when RouterOS would otherwise include them in /container.
func (c *Client) ContainerRead(ctx context.Context, menu ContainerMenu) ([]RouterOSObject, error) {
	props := ""
	switch menu {
	case ContainerList:
		props = ".id,name,remote-image,file,tag,interface,root-dir,cmd,entrypoint,user,workdir,start-on-boot,logging,restart-policy,cpu-list,cpu-usage,memory-usage,memory-current,memory-high,memory-max,running,healthy,unhealthy,stopped,starting,starting-with-healthcheck,stopping,extracting,downloading,downloading/extracting,download/extract failed,error,status,envlists,envlist,mountlists,mounts,healthcheck-cmd,healthcheck-interval,healthcheck-timeout,healthcheck-retries,healthcheck-start-period,default-cmd,default-entrypoint,default-user,default-workdir,default-healthcheck-cmd"
	case ContainerConfig:
		props = "memory-high,memory-max"
	case ContainerVETH:
		props = ".id,name,address,gateway,gateway6,mac-address,dhcp"
	case ContainerBridge:
		props = "name,disabled"
	case ContainerBridgePort:
		props = "interface,bridge,disabled"
	case ContainerInterfaces:
		props = "name"
	case ContainerAddresses:
		props = "address,interface"
	case ContainerDisk:
		props = "slot,name,mount-point,free,total,fs,type,disabled,mounted,formatting,empty"
	case ContainerEnvs:
		props = "list,key,value"
	case ContainerMounts:
		props = "list,name,src,dst,mode"
	case ContainerResource:
		props = "version,architecture-name"
	case ContainerFiles:
		props = ".id,name,type,size"
	case ContainerLogs:
		props = ".id,container,time,message"
	default:
		return nil, errors.New("unsupported container read menu")
	}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/rest/"+string(menu)+"?.proplist="+url.QueryEscape(props), &raw); err != nil {
		return nil, err
	}
	rows := []RouterOSObject{}
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("decode container records: %w", err)
		}
	} else {
		var row RouterOSObject
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("decode container singleton: %w", err)
		}
		if row != nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
