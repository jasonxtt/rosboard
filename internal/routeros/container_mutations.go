package routeros

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ContainerCommand exposes only native container objects. It deliberately has
// no bridge configuration, IP address, firewall, route or arbitrary menu access.
func (c *MutationClient) ContainerCommand(ctx context.Context, menu ContainerMenu, command string, fields RouterOSFields) error {
	allowed := map[ContainerMenu]string{
		ContainerList:       "name,remote-image,file,interface,root-dir,cmd,entrypoint,user,workdir,start-on-boot,logging,restart-policy,cpu-list,memory-high,memory-max,healthcheck-cmd,healthcheck-interval,healthcheck-timeout,healthcheck-retries,healthcheck-start-period,envlists,mountlists,comment",
		ContainerVETH:       "name,address,gateway,gateway6,mac-address,comment",
		ContainerBridgePort: "interface,bridge,comment",
		ContainerEnvs:       "list,key,value,comment",
		ContainerMounts:     "list,src,dst,mode,comment",
	}
	properties, ok := allowed[menu]
	if !ok {
		return errors.New("unsupported container mutation menu")
	}
	switch command {
	case "add", "set", "remove":
	case "start", "stop":
		if menu != ContainerList {
			return errors.New("unsupported lifecycle menu")
		}
	default:
		return errors.New("unsupported container command")
	}
	if command != "add" {
		id, ok := fields["numbers"].(string)
		if !ok {
			return errors.New("invalid RouterOS ID")
		}
		if err := validateMutationID(id); err != nil {
			return err
		}
	}
	permitted := map[string]bool{}
	for _, k := range strings.Split(properties, ",") {
		permitted[k] = true
	}
	for k := range fields {
		if k == "numbers" && command != "add" {
			continue
		}
		if command == "remove" || command == "start" || command == "stop" || !permitted[k] {
			return errors.New("unsupported container mutation property")
		}
	}
	if value, exists := fields["cpu-list"]; exists {
		cpu, ok := value.(string)
		if !ok {
			return errors.New("invalid CPU list")
		}
		projected, err := ContainerCPUList(cpu)
		if err != nil {
			return err
		}
		copy := RouterOSFields{}
		for k, v := range fields {
			copy[k] = v
		}
		copy["cpu-list"] = projected
		fields = copy
	}
	for _, key := range []string{"memory-high", "memory-max"} {
		if value, exists := fields[key]; exists && value == "" {
			copy := RouterOSFields{}
			for k, v := range fields {
				copy[k] = v
			}
			copy[key] = "unlimited"
			fields = copy
		}
	}
	endpoint := c.baseURL + "/rest/" + string(menu) + "/" + command
	body, err := c.execute(ctx, http.MethodPost, endpoint, fields, maxMutationJSONBytes, mutationNoRetryMutation)
	if err != nil {
		return err
	}
	if _, err := decodeMutationResponse(body); err != nil {
		return mutationUnknownFor(http.MethodPost, endpoint, 0)
	}
	return nil
}

// RouterOS identifies CPU cores by cpu0/cpu1 names, rather than Docker's
// numeric CPU list. Bound expansion prevents unbounded ranges in requests.
func ContainerCPUList(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	cores := []string{}
	seen := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		ends := strings.Split(part, "-")
		if len(ends) > 2 {
			return "", errors.New("invalid CPU range")
		}
		first, err := strconv.Atoi(strings.TrimPrefix(ends[0], "cpu"))
		if err != nil || first < 0 || first >= 256 {
			return "", errors.New("invalid CPU number")
		}
		last := first
		if len(ends) == 2 {
			last, err = strconv.Atoi(ends[1])
			if err != nil || last < first || last >= 256 {
				return "", errors.New("invalid CPU range")
			}
		}
		for i := first; i <= last; i++ {
			if !seen[i] {
				cores = append(cores, fmt.Sprintf("cpu%d", i))
				seen[i] = true
			}
		}
	}
	return strings.Join(cores, ","), nil
}
