package containers

import (
	"encoding/csv"
	"errors"
	"path"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseContainerImport keeps the original Compose endpoint compatible while
// accepting a single docker run command. Neither syntax invokes a shell.
func ParseContainerImport(source string) (ComposeResult, error) {
	source = strings.TrimSpace(strings.TrimPrefix(source, "\ufeff"))
	if strings.HasPrefix(source, "$ ") {
		source = strings.TrimSpace(source[2:])
	}
	first := strings.Fields(source)
	if len(first) > 0 && (first[0] == "docker" || first[0] == "sudo") {
		return ParseDockerRun(source)
	}
	return ParseCompose(source)
}

type dockerWord struct {
	value   string
	dynamic bool
}

// Tokenization supports quoting and escaped newlines, without evaluating any
// substitutions. Dynamic unquoted/double-quoted values need manual input.
func dockerWords(source string) ([]dockerWord, error) {
	words := []dockerWord{}
	var value strings.Builder
	quote := byte(0)
	started, dynamic := false, false
	flush := func() {
		if started {
			words = append(words, dockerWord{value.String(), dynamic})
			value.Reset()
			started, dynamic = false, false
		}
	}
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c == 0 {
			return nil, errors.New("Docker 命令不能包含 NUL")
		}
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			} else {
				value.WriteByte(c)
			}
			continue
		}
		if c == '\\' {
			if i+1 == len(source) {
				return nil, errors.New("Docker 命令末尾的反斜杠不完整")
			}
			next := source[i+1]
			if next == '\n' || next == '\r' && i+2 < len(source) && source[i+2] == '\n' {
				i++
				if next == '\r' {
					i++
				}
				continue
			}
			if quote == '"' && next != '$' && next != '`' && next != '"' && next != '\\' {
				value.WriteByte(c)
			} else {
				i++
				value.WriteByte(next)
			}
			started = true
			continue
		}
		if c == '`' || c == '$' && i+1 < len(source) && source[i+1] == '(' {
			return nil, errors.New("暂不支持 Shell 命令替换，请粘贴实际参数；不会执行命令")
		}
		if c == '$' && i+1 < len(source) {
			next := source[i+1]
			if next == '{' || next == '$' || next == '_' || next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' || strings.ContainsRune("0123456789?!#*-@", rune(next)) {
				dynamic = true
			}
		}
		if quote == '"' {
			if c == '"' {
				quote = 0
			} else {
				value.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case ' ', '\t':
			flush()
		case '#':
			if !started {
				return nil, errors.New("请移除 Shell 注释，只粘贴单条 docker run 命令")
			}
			value.WriteByte(c)
		case '\n', '\r', ';', '|', '&', '<', '>', '(', ')':
			return nil, errors.New("请只粘贴一条 docker run 命令；多行参数使用反斜杠续行，不支持管道、重定向或其他 Shell 语句")
		default:
			if c == '~' && !started || c == '*' || c == '?' || c == '[' {
				dynamic = true
			}
			value.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("Docker 命令引号未闭合")
	}
	flush()
	if len(words) > 2048 {
		return nil, errors.New("Docker 参数过多，请缩小后再导入")
	}
	return words, nil
}

// Known option arity is essential: guessing whether an unknown flag consumes a
// value could mistake that value for the image. Unknown flags fail closed.
var dockerValueFlags = strings.Fields(`add-host annotation attach blkio-weight blkio-weight-device cap-add cap-drop cgroup-parent cgroupns cidfile cpu-count cpu-percent cpu-period cpu-quota cpu-rt-period cpu-rt-runtime cpu-shares cpus cpuset-cpus cpuset-mems detach-keys device device-cgroup-rule device-read-bps device-read-iops device-write-bps device-write-iops dns dns-option dns-search domainname entrypoint env env-file expose gpus group-add health-cmd health-interval health-retries health-start-interval health-start-period health-timeout hostname io-maxbandwidth io-maxiops ip ip6 ipc isolation label label-file link link-local-ip log-driver log-opt mac-address memory memory-reservation memory-swap memory-swappiness mount name network network-alias oom-score-adj pid pids-limit platform pull restart runtime security-opt shm-size stop-signal stop-timeout storage-opt sysctl tmpfs ulimit umask user userns uts volume volume-driver volumes-from workdir`)
var dockerBoolFlags = strings.Fields(`detach interactive tty privileged publish-all read-only rm init no-healthcheck oom-kill-disable sig-proxy disable-content-trust help quiet use-api-socket`)
var dockerShortFlags = map[byte]string{'a': "attach", 'c': "cpu-shares", 'd': "detach", 'e': "env", 'h': "hostname", 'i': "interactive", 'l': "label", 'm': "memory", 'p': "publish", 'P': "publish-all", 'q': "quiet", 't': "tty", 'u': "user", 'v': "volume", 'w': "workdir"}

func dockerFlag(name string) (string, bool, bool) {
	if name == "net" {
		name = "network"
	}
	if name == "publish" {
		return name, true, true
	}
	for _, candidate := range dockerValueFlags {
		if candidate == name {
			return name, true, true
		}
	}
	for _, candidate := range dockerBoolFlags {
		if candidate == name {
			return name, false, true
		}
	}
	return "", false, false
}

func dockerString(value string) *yaml.Node {
	// Compose's literal-dollar escape must not alter Docker's already-tokenized
	// single-quoted or escaped literals (including passwords containing dollars).
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.ReplaceAll(value, "$", "$$")}
}

func ParseDockerRun(source string) (ComposeResult, error) {
	out := ComposeResult{Format: "docker-run", Services: []ComposeService{}, Skipped: []ComposeNotice{}}
	if len(source) == 0 || len(source) > MaxComposeBytes {
		return out, errors.New("请粘贴 Docker run 命令，最多支持 48 KiB")
	}
	words, err := dockerWords(strings.TrimSpace(source))
	if err != nil {
		return out, err
	}
	pos := 0
	if len(words) > 0 && words[0].value == "sudo" {
		pos++
	}
	if pos >= len(words) || words[pos].value != "docker" {
		return out, errors.New("仅支持 docker run 或 docker container run")
	}
	pos++
	if pos < len(words) && words[pos].value == "container" {
		pos++
	}
	if pos >= len(words) || words[pos].value != "run" {
		return out, errors.New("仅支持 docker run 或 docker container run")
	}
	pos++
	s := ComposeService{Name: "docker-run", Fields: ComposeFields{RestartPolicy: "no", Env: []Environment{}, Mounts: []Mount{}}, Imported: []string{}, Skipped: []ComposeNotice{}, NeedsInput: []ComposeNotice{}, MountHints: []string{}}
	volumes := &yaml.Node{Kind: yaml.SequenceNode}
	envOrder := []string{}
	envs := map[string]*yaml.Node{}
	apply := func(flag string, value dockerWord) {
		field := "--" + flag
		// Single-value Docker flags use their last occurrence. Never retain an
		// earlier override when its replacement is unresolved or unsupported.
		switch flag {
		case "name":
			s.Fields.Name = ""
		case "restart":
			s.Fields.RestartPolicy = "no"
		case "user":
			s.Fields.User = ""
		case "workdir":
			s.Fields.Workdir = ""
		case "entrypoint":
			s.Fields.Entrypoint = ""
		case "memory":
			s.Fields.MemoryMax = ""
		case "memory-reservation":
			s.Fields.MemoryHigh = ""
		case "cpuset-cpus":
			s.Fields.CPUList = ""
		}
		if value.dynamic {
			s.need(field, "包含 Shell 变量，请填写实际值；不会读取宿主机环境")
			if flag != "env" && flag != "volume" && flag != "mount" {
				return
			}
		}
		switch flag {
		case "env":
			key, val, hasValue := strings.Cut(value.value, "=")
			if !envPattern.MatchString(key) {
				s.need(field, "变量名无效或包含未解析变量，请手动填写")
				return
			}
			if _, found := envs[key]; !found {
				envOrder = append(envOrder, key)
			}
			if !hasValue || value.dynamic {
				envs[key] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
			} else {
				envs[key] = dockerString(val)
			}
		case "volume":
			parts := strings.Split(value.value, ":")
			if value.dynamic && (len(parts) < 2 || strings.Contains(parts[1], "$")) {
				return
			}
			volumes.Content = append(volumes.Content, dockerString(value.value))
		case "mount":
			if mount := s.dockerMount(value); mount != nil {
				volumes.Content = append(volumes.Content, mount)
			}
		case "name", "restart", "user", "workdir", "entrypoint", "memory", "memory-reservation", "cpuset-cpus":
			key := map[string]string{"name": "container_name", "workdir": "working_dir", "memory": "mem_limit", "memory-reservation": "mem_reservation", "cpuset-cpus": "cpuset"}[flag]
			if key == "" {
				key = flag
			}
			s.convert(key, dockerString(value.value))
		case "network", "ip", "ip6", "mac-address", "hostname", "network-alias", "link":
			s.skip(field, "ROS 使用独立 VETH 和容器 IP，此 Docker 网络设置未应用；不提供 Docker 服务名解析")
			s.need("network", "请在面板选择 ROS bridge，并填写容器 IP 与网关")
		case "publish", "publish-all", "expose":
			s.skip(field, "不创建端口映射或 NAT，请通过容器 IP 和容器内端口访问")
		case "detach", "interactive", "tty", "quiet", "sig-proxy", "detach-keys":
			s.skip(field, "面板后台管理容器；启动行为沿用面板选择，不导入 Docker 终端设置")
		case "env-file", "label-file":
			s.skip(field, "不读取宿主机文件，请在面板补齐实际配置")
			s.need(field, "请手动填写文件中需要的配置")
		default:
			s.skip(field, "当前面板不支持此 Docker 参数，已跳过，请核对应用是否依赖此设置")
		}
	}
	for pos < len(words) && strings.HasPrefix(words[pos].value, "-") && words[pos].value != "-" {
		word := words[pos]
		pos++
		if word.value == "--" {
			break
		}
		if strings.HasPrefix(word.value, "--") {
			name, value, assigned := strings.Cut(word.value[2:], "=")
			name, takesValue, known := dockerFlag(name)
			if !known {
				return out, errors.New("存在暂不认识的 Docker 参数，请移除后再解析；不会猜测参数与镜像的边界")
			}
			arg := dockerWord{value, word.dynamic}
			if takesValue && !assigned {
				if pos >= len(words) {
					return out, errors.New("Docker 参数缺少值")
				}
				arg = words[pos]
				pos++
			} else if !takesValue {
				if !assigned {
					arg.value = "true"
				} else if _, err := strconv.ParseBool(arg.value); err != nil {
					return out, errors.New("Docker 布尔参数需使用 true 或 false")
				}
			}
			apply(name, arg)
			continue
		}
		short := word.value[1:]
		for len(short) > 0 {
			name, known := dockerShortFlags[short[0]]
			if !known {
				return out, errors.New("存在暂不认识的 Docker 短参数，请移除后再解析")
			}
			_, takesValue, _ := dockerFlag(name)
			short = short[1:]
			arg := dockerWord{"true", word.dynamic}
			if takesValue {
				if len(short) > 0 {
					arg.value = strings.TrimPrefix(short, "=")
					short = ""
				} else {
					if pos >= len(words) {
						return out, errors.New("Docker 短参数缺少值")
					}
					arg = words[pos]
					pos++
				}
			} else if strings.HasPrefix(short, "=") {
				arg.value = short[1:]
				short = ""
				if _, err := strconv.ParseBool(arg.value); err != nil {
					return out, errors.New("Docker 布尔短参数需使用 true 或 false")
				}
			}
			apply(name, arg)
		}
	}
	if pos >= len(words) || words[pos].value == "" {
		return out, errors.New("Docker run 缺少镜像名称")
	}
	image := words[pos]
	if image.dynamic {
		s.need("image", "镜像包含 Shell 变量，请填写实际镜像名称")
	} else {
		s.convert("image", dockerString(image.value))
	}
	if s.Fields.Name == "" {
		s.Fields.Name = strings.SplitN(strings.SplitN(path.Base(s.Fields.Image), "@", 2)[0], ":", 2)[0]
		if s.Fields.Name == "" || s.Fields.Name == "." {
			s.Fields.Name = "container"
		}
	}
	s.Name = s.Fields.Name
	if len(envOrder) > 0 {
		mapping := &yaml.Node{Kind: yaml.MappingNode}
		for _, key := range envOrder {
			mapping.Content = append(mapping.Content, dockerString(key), envs[key])
		}
		s.environment(mapping)
	}
	if len(volumes.Content) > 0 {
		s.volumes(volumes)
		for i := range s.MountHints {
			s.MountHints[i] = strings.ReplaceAll(s.MountHints[i], "$$", "$")
		}
	}
	if pos+1 < len(words) {
		s.skip("command", "镜像后的启动参数未自动拼接，以免改变参数边界")
		s.need("command", "请在面板核对并手动填写 CMD；保留引号和参数边界")
	} else if s.Fields.Entrypoint != "" {
		s.need("command", "Docker 覆盖 entrypoint 会影响镜像默认 CMD，请核对面板的命令继承行为")
	}
	out.Services = append(out.Services, s)
	return out, nil
}

func (s *ComposeService) dockerMount(word dockerWord) *yaml.Node {
	reader := csv.NewReader(strings.NewReader(word.value))
	records, err := reader.ReadAll()
	if err != nil || len(records) != 1 {
		s.skip("--mount", "挂载语法无法转换，请手动添加")
		return nil
	}
	parts := records[0]
	values := map[string]string{"type": "volume"}
	readOnly := false
	for _, part := range parts {
		key, value, assigned := strings.Cut(part, "=")
		switch key {
		case "type":
			values["type"] = value
		case "src", "source":
			values["source"] = value
		case "dst", "destination", "target":
			values["target"] = value
		case "ro", "readonly":
			if !assigned {
				readOnly = true
			} else if parsed, err := strconv.ParseBool(value); err == nil {
				readOnly = parsed
			} else {
				s.skip("--mount", "readonly 值无效，请手动添加挂载")
				return nil
			}
		default:
			s.skip("--mount.option", "额外挂载选项不导入")
		}
	}
	if word.dynamic && strings.Contains(values["target"], "$") {
		s.need("--mount.target", "挂载目标包含未解析变量，请手动添加挂载")
		return nil
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range []string{"type", "source", "target"} {
		n.Content = append(n.Content, dockerString(key), dockerString(values[key]))
	}
	n.Content = append(n.Content, dockerString("read_only"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(readOnly)})
	return n
}
