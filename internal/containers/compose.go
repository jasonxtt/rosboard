package containers

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"rosboard/internal/routeros"
)

const MaxComposeBytes = 48 << 10

// ComposeFields deliberately excludes ROS identities, paths and networking.
type ComposeFields struct {
	Name          string        `json:"name"`
	Image         string        `json:"image"`
	Env           []Environment `json:"env"`
	Mounts        []Mount       `json:"mounts"`
	Command       string        `json:"command"`
	Entrypoint    string        `json:"entrypoint"`
	User          string        `json:"user"`
	Workdir       string        `json:"workdir"`
	MemoryHigh    string        `json:"memoryHigh"`
	MemoryMax     string        `json:"memoryMax"`
	CPUList       string        `json:"cpuList"`
	RestartPolicy string        `json:"restartPolicy"`
}

type ComposeNotice struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ComposeService struct {
	Name       string          `json:"name"`
	Fields     ComposeFields   `json:"fields"`
	Imported   []string        `json:"imported"`
	Skipped    []ComposeNotice `json:"skipped"`
	NeedsInput []ComposeNotice `json:"needsInput"`
	MountHints []string        `json:"mountHints"`
}

type ComposeResult struct {
	Format   string           `json:"format,omitempty"`
	Services []ComposeService `json:"services"`
	Skipped  []ComposeNotice  `json:"skipped"`
}

// ParseCompose is pure. Source and decoder errors must never reach logs/errors:
// YAML decoder diagnostics can contain environment values and other secrets.
func ParseCompose(source string) (ComposeResult, error) {
	out := ComposeResult{Format: "compose", Services: []ComposeService{}, Skipped: []ComposeNotice{}}
	if len(source) == 0 || len(source) > MaxComposeBytes {
		return out, errors.New("请粘贴 Compose 内容，最多支持 48 KiB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(source))
	var doc, extra yaml.Node
	if decoder.Decode(&doc) != nil || len(doc.Content) != 1 {
		return out, errors.New("Compose YAML 语法无效，请检查缩进和引号")
	}
	if decoder.Decode(&extra) != io.EOF {
		return out, errors.New("一次只能导入一份 YAML 文档")
	}
	count := 0
	if err := validateComposeNode(&doc, 0, &count); err != nil {
		return out, err
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return out, errors.New("Compose 顶层必须包含 services 映射")
	}
	services := composeMember(root, "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 || len(services.Content) > 128 {
		return out, errors.New("services 必须包含 1 至 64 个服务")
	}
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if key != "services" {
			out.Skipped = append(out.Skipped, ComposeNotice{key, "顶层配置不导入；仅填写所选服务，不创建网络、卷或其他服务"})
		}
	}
	for i := 0; i < len(services.Content); i += 2 {
		name, body := services.Content[i].Value, services.Content[i+1]
		if body.Kind != yaml.MappingNode {
			return out, errors.New("每个服务都必须是字段映射")
		}
		s := ComposeService{Name: name, Fields: ComposeFields{Name: name, Env: []Environment{}, Mounts: []Mount{}, RestartPolicy: "no"}, Imported: []string{}, Skipped: []ComposeNotice{}, NeedsInput: []ComposeNotice{}, MountHints: []string{}}
		s.Imported = append(s.Imported, "名称（container_name 或服务名）")
		for j := 0; j < len(body.Content); j += 2 {
			s.convert(body.Content[j].Value, body.Content[j+1])
		}
		if s.Fields.Image == "" {
			s.need("image", "请填写可下载的镜像名称；不会执行 build")
		}
		if s.Fields.Entrypoint != "" && s.Fields.Command == "" {
			s.need("command", "Compose 自定义 entrypoint 会忽略镜像 CMD；请核对面板中的命令继承行为")
		}
		out.Services = append(out.Services, s)
	}
	return out, nil
}

func validateComposeNode(n *yaml.Node, depth int, count *int) error {
	*count++
	if depth > 32 || *count > 4096 {
		return errors.New("Compose 结构过于复杂，请缩小后再导入")
	}
	if n.Kind == yaml.AliasNode {
		return errors.New("暂不支持 YAML 别名和合并键，请展开配置后再导入")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return fmt.Errorf("第 %d 行：字段名须为字符串，不支持合并键", key.Line)
			}
			if seen[key.Value] {
				return fmt.Errorf("第 %d 行：存在重复字段", key.Line)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := validateComposeNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func composeMember(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func (s *ComposeService) skip(field, message string) {
	s.Skipped = append(s.Skipped, ComposeNotice{field, message})
}
func (s *ComposeService) need(field, message string) {
	s.NeedsInput = append(s.NeedsInput, ComposeNotice{field, message})
}

// No external interpolation is performed. Only Compose's literal-dollar escape
// is decoded. Unresolved expressions must not silently become empty strings.
func composeLiteral(value string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '$' && i+1 < len(value) {
			next := value[i+1]
			if next == '$' {
				b.WriteByte('$')
				i++
				continue
			}
			if next == '{' || next == '_' || next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
				return "", false
			}
		}
		b.WriteByte(value[i])
	}
	return b.String(), true
}

func (s *ComposeService) scalar(field string, n *yaml.Node, textOnly bool) (string, bool) {
	if n == nil || n.Tag == "!!null" {
		return "", false
	}
	if n.Kind != yaml.ScalarNode || textOnly && n.Tag != "!!str" {
		s.skip(field, "仅支持字符串配置，请在面板手动填写")
		return "", false
	}
	value := n.Value
	if n.Tag != "!!str" {
		if n.Tag != "!!int" && n.Tag != "!!float" && n.Tag != "!!bool" {
			s.skip(field, "不支持此值类型，请手动填写")
			return "", false
		}
		var native any
		if n.Decode(&native) != nil {
			s.skip(field, "无法转换此值，请手动填写")
			return "", false
		}
		value = fmt.Sprint(native)
	}
	value, ok := composeLiteral(value)
	if !ok {
		s.need(field, "包含变量表达式，请填写实际值；不会读取宿主机变量或 .env")
	}
	return value, ok
}

func (s *ComposeService) convert(key string, n *yaml.Node) {
	switch key {
	case "environment":
		s.environment(n)
	case "volumes":
		s.volumes(n)
	case "image", "container_name", "user", "working_dir", "command", "entrypoint", "restart", "mem_limit", "mem_reservation", "cpuset":
		value, ok := s.scalar(key, n, key != "user" && key != "mem_limit" && key != "mem_reservation" && key != "cpuset")
		if !ok {
			if (key == "command" || key == "entrypoint") && n.Tag != "!!null" {
				s.need(key, "启动覆盖未导入，请核对并手动填写；不会拼接参数列表")
			}
			return
		}
		switch key {
		case "image":
			s.Fields.Image = strings.TrimSpace(value)
		case "container_name":
			s.Fields.Name = value
		case "user":
			s.Fields.User = value
		case "working_dir":
			s.Fields.Workdir = value
		case "command", "entrypoint":
			if value == "" {
				s.skip(key, "显式清空启动设置无法等价导入，请手动配置")
				s.need(key, "面板留空会继承镜像启动设置，请核对")
				return
			}
			if key == "command" {
				s.Fields.Command = value
			} else {
				s.Fields.Entrypoint = value
			}
		case "restart":
			if value != "no" && value != "always" && value != "on-failure" {
				s.skip(key, "ROS 不支持此重启策略，保留面板默认 no；可手动选择其他策略")
				return
			}
			s.Fields.RestartPolicy = value
		case "mem_limit", "mem_reservation":
			memory, ok := composeMemory(value)
			if !ok {
				s.skip(key, "内存值无法转换，请在面板按 MB 填写")
				return
			}
			if key == "mem_limit" {
				s.Fields.MemoryMax = memory
			} else {
				s.Fields.MemoryHigh = memory
			}
		case "cpuset":
			if value == "" {
				s.skip(key, "CPU 绑定不能为空，请在面板填写编号或范围")
				return
			}
			if _, err := routeros.ContainerCPUList(value); err != nil {
				s.skip(key, "CPU 绑定格式无效，请在面板填写编号或范围")
				return
			}
			s.Fields.CPUList = value
		}
		s.Imported = append(s.Imported, key)
	case "ports", "expose":
		s.skip(key, "不生成端口映射或 NAT，请通过容器 IP 和容器内端口访问")
	case "networks", "network_mode", "hostname", "links", "depends_on":
		s.skip(key, "不创建网络、依赖服务或服务名解析；请手动填写 ROS 网络并核对依赖地址")
	case "env_file", "configs", "secrets", "extends", "build":
		s.skip(key, "不读取外部文件、不构建镜像，请补齐镜像和实际配置")
		s.need(key, "请在表单补齐此配置所提供的信息")
	default:
		s.skip(key, "当前面板不支持，已跳过；请核对应用是否依赖此设置")
	}
}

func (s *ComposeService) environment(n *yaml.Node) {
	if n.Kind != yaml.MappingNode && n.Kind != yaml.SequenceNode {
		s.skip("environment", "环境变量需使用映射或列表")
		return
	}
	seen := map[string]bool{}
	add := func(key, value string) {
		if !envPattern.MatchString(key) || seen[key] {
			s.need("environment", "存在无效或重复变量名，请手动检查环境变量")
			return
		}
		seen[key] = true
		s.Fields.Env = append(s.Fields.Env, Environment{Key: key, Value: value})
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			key, node := n.Content[i].Value, n.Content[i+1]
			if node.Tag == "!!null" {
				s.need("environment."+key, "未提供值，请手动填写")
				continue
			}
			if value, ok := s.scalar("environment."+key, node, false); ok {
				add(key, value)
			}
		}
	} else {
		for i, node := range n.Content {
			field := fmt.Sprintf("environment[%d]", i)
			if value, ok := s.scalar(field, node, true); ok {
				key, value, found := strings.Cut(value, "=")
				if !found {
					s.need(field, "未提供变量值，请手动填写")
					continue
				}
				add(key, value)
			}
		}
	}
	s.Imported = append(s.Imported, "environment")
}

func (s *ComposeService) volumes(n *yaml.Node) {
	if n.Kind != yaml.SequenceNode {
		s.skip("volumes", "挂载需使用列表")
		return
	}
	for i, node := range n.Content {
		field := fmt.Sprintf("volumes[%d]", i)
		var source, target string
		readOnly := false
		if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
			parts := strings.Split(node.Value, ":")
			if len(parts) == 1 {
				target = parts[0]
			} else if len(parts) == 2 || len(parts) == 3 {
				source, target = parts[0], parts[1]
				if len(parts) == 3 {
					for _, mode := range strings.Split(parts[2], ",") {
						switch mode {
						case "ro":
							readOnly = true
						case "rw":
						default:
							s.skip(field+".mode", "仅导入 ro/rw，其他挂载选项已跳过")
						}
					}
				}
			} else {
				s.skip(field, "挂载格式无法转换，请手动添加")
				continue
			}
		} else if node.Kind == yaml.MappingNode {
			kind := composeMember(node, "type")
			if kind == nil || kind.Kind != yaml.ScalarNode || kind.Value != "bind" && kind.Value != "volume" {
				s.skip(field, "仅导入 bind 和 volume 挂载")
				continue
			}
			if v := composeMember(node, "source"); v != nil && v.Kind == yaml.ScalarNode {
				source = v.Value
			}
			if v := composeMember(node, "target"); v != nil && v.Kind == yaml.ScalarNode {
				target = v.Value
			}
			if v := composeMember(node, "read_only"); v != nil {
				if v.Tag != "!!bool" || v.Decode(&readOnly) != nil {
					s.skip(field, "read_only 需为布尔值，请手动添加此挂载")
					continue
				}
			}
			for j := 0; j < len(node.Content); j += 2 {
				key := node.Content[j].Value
				if key != "type" && key != "source" && key != "target" && key != "read_only" {
					s.skip(field+"."+key, "额外挂载选项不导入")
				}
			}
		} else {
			s.skip(field, "挂载格式无法转换，请手动添加")
			continue
		}
		literal, ok := composeLiteral(target)
		if !ok || !strings.HasPrefix(literal, "/") || literal == "/" {
			s.need(field, "请手动添加此挂载，目标需为实际容器内绝对路径")
			continue
		}
		mode := "rw"
		if readOnly {
			mode = "ro"
		}
		s.Fields.Mounts = append(s.Fields.Mounts, Mount{Target: literal, ReadOnly: readOnly, Mode: mode})
		s.MountHints = append(s.MountHints, source)
		s.need(field+".source", "请选择 ROS 来源目录；原来源仅供参考，不迁移数据")
	}
	s.Imported = append(s.Imported, "volumes（目标及读写模式）")
}

var composeMemoryPattern = regexp.MustCompile(`(?i)^([0-9]+)(b|k|kb|m|mb|g|gb)?$`)

func composeMemory(value string) (string, bool) {
	parts := composeMemoryPattern.FindStringSubmatch(strings.TrimSpace(value))
	if parts == nil {
		return "", false
	}
	n, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return "", false
	}
	shift := uint(0)
	switch strings.ToLower(parts[2]) {
	case "k", "kb":
		shift = 10
	case "m", "mb":
		shift = 20
	case "g", "gb":
		shift = 30
	}
	if n > uint64(^uint64(0)>>1)>>shift {
		return "", false
	}
	if n == 0 {
		return "unlimited", true
	}
	return strconv.FormatUint(n<<shift, 10), true
}
