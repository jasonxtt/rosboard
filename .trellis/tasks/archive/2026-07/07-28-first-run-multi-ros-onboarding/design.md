# 首次安装初始化与多 ROS 设备配置：技术设计

## 1. Design goals

本设计把“是否可访问面板”“初始化进行到哪一步”“某台 RouterOS 是否刚刚通过验证”拆成三个独立事实：

1. SQLite 中是否存在唯一管理员。
2. SQLite 中初始化完成标记是否为真。
3. 当前设备连接草稿是否持有未过期且与草稿完全绑定的验证票据。

浏览器只渲染服务端返回的状态，不自行决定初始化是否完成。RouterOS 正式设备配置继续保存在 YAML；管理员、会话和初始化状态保存在 SQLite。

本任务保持单一集成任务，不拆成子任务。认证中间件、初始化状态、设备保存合同和前端路由共享同一组 API 状态，拆分会造成大量交叉修改和难以独立验收。

## 2. State model

### 2.1 Application phases

服务端启动状态投影为：

| Phase | Admin exists | Authenticated | Onboarding complete | Frontend |
|---|---:|---:|---:|---|
| needs_admin | no | no | no | 创建管理员 |
| needs_login | yes | no | either | 登录 |
| needs_routeros | yes | yes | no | RouterOS 初始化步骤 |
| ready | yes | yes | yes | 常规面板，可无设备 |

GET /api/bootstrap 是唯一前端启动判定入口，只返回 phase、authenticated、onboardingComplete 和已认证时的管理员显示信息，不返回密码、哈希、设备凭据或内部票据。

### 2.2 Transitions

- needs_admin → needs_routeros：首个管理员事务创建成功，同时建立会话。
- needs_routeros → ready：管理员在设备编辑页点击“完成设置”并成功持久化当前设备，或在选择页显式调用跳过/进入面板。
- needs_routeros 页面先渲染添加/跳过选择；添加路径进入带设备列表和新增入口的设备编辑页，编辑页不再重复显示跳过动作。
- needs_login → needs_routeros|ready：登录后按持久化初始化状态路由。
- ready 永不因设备列表为空退回初始化；清空全部设备只产生空面板。

如果 YAML 保存成功但初始化完成标记更新失败，系统仍停留在 RouterOS 步骤并显示已存在设备，管理员可再次完成；不得先写完成标记再保存 YAML。

## 3. Persistence

### 3.1 SQLite schema

在现有 rosboard.db 中增加独立于设备采样表的认证表：

    CREATE TABLE IF NOT EXISTS admin_account (
      id INTEGER PRIMARY KEY CHECK (id = 1),
      username TEXT NOT NULL UNIQUE,
      password_hash BLOB NOT NULL,
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS auth_sessions (
      token_hash BLOB PRIMARY KEY,
      admin_id INTEGER NOT NULL,
      created_at INTEGER NOT NULL,
      last_seen INTEGER NOT NULL,
      expires_at INTEGER NOT NULL,
      FOREIGN KEY (admin_id) REFERENCES admin_account(id) ON DELETE CASCADE
    );

    CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at
      ON auth_sessions(expires_at);

    CREATE TABLE IF NOT EXISTS app_state (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL,
      updated_at INTEGER NOT NULL
    );

app_state['onboarding_complete'] == 'true' 表示初始化完成。管理员存在与否从 admin_account 查询，不复制第二个布尔值。

首个管理员使用单事务 INSERT id=1；主键和唯一约束是并发最终防线。所有认证存储方法作用于 owner Store，不接受设备作用域。

### 3.2 Password hashing

- 使用 golang.org/x/crypto/argon2 的 Argon2id；每个密码使用独立 16-byte 随机 salt，建议参数为 64 MiB memory、3 iterations、parallelism 2、32-byte output，并把算法版本与参数编码进 hash 字符串。
- 选择 Argon2id 而不是 bcrypt，是因为需求允许最多 128 个 Unicode 字符，不能受 bcrypt 72-byte 上限影响。
- 长度按 Unicode 字符计数，确认校验在哈希前完成；密码按原始 UTF-8 字节处理，不 trim。
- 验证使用 constant-time compare；API、日志和错误包装均不得包含密码、salt 或 hash。

### 3.3 Sessions

- 登录生成 32 字节密码学随机 token；Cookie 只保存 base64url token。
- SQLite 只保存 SHA-256(token)，数据库泄漏不能直接复用会话。
- Cookie 名固定为 rosboard_session，属性为 HttpOnly、SameSite=Strict、Path=/、Max-Age=7d；TLS 请求设置 Secure。
- 每次认证读取校验 expires_at；接近续期阈值时把 last_seen 与 expires_at 滚动到当前时间 + 7 天，并刷新 Cookie，避免每个请求写库。
- 退出删除当前 token hash。修改/重置密码删除该管理员全部会话。
- 启动和认证路径惰性删除过期会话。

### 3.4 Login throttling

使用进程内限速器，键为 RemoteAddr IP + 分隔符 + trimmed username：

- 10 分钟窗口内前 5 次失败不冷却。
- 从第 5 次开始冷却 30 秒，后续失败指数增长，最大 15 分钟。
- 全局最多并发执行 2 个 Argon2id 验证；超出时快速返回限速响应，避免并发请求耗尽内存。
- 成功登录清除对应键。
- 返回 429 和 Retry-After 时只提示稍后重试；普通失败统一返回 401“账号或密码错误”。
- 不写永久锁定状态；进程重启清空失败计数是可接受的安全/复杂度取舍。

## 4. HTTP boundaries

### 4.1 Request ordering

所有 /api/* 请求按以下顺序处理：

1. allowed_cidrs 来源检查。
2. 安全响应头和同源写请求检查。
3. 公开路由或会话认证。
4. 初始化 phase 授权。
5. 业务处理。

公开 API 仅包括：

- GET /api/health
- GET /api/bootstrap
- POST /api/setup/admin（仅无管理员）
- POST /api/auth/login

POST /api/auth/logout 及其他 API 均要求认证。未认证返回 401；已认证但 phase 不允许返回 409 及稳定错误码。

所有写请求验证 Origin 与当前 Host 同源；缺失 Origin 的非浏览器本机 CLI 路径不复用 HTTP 管理接口。配合 SameSite=Strict Cookie 防止跨站请求伪造。

### 4.2 Auth/setup contracts

    GET  /api/bootstrap
    POST /api/setup/admin
         { username, password, passwordConfirmation }

    POST /api/auth/login
         { username, password }
    POST /api/auth/logout

    PUT  /api/account
         { username, password, passwordConfirmation }

    POST /api/setup/complete
         { skipRouterOS: boolean }

skipRouterOS=true 表示选择页明确跳过；false 只在 YAML 已存在至少一台有效设备时允许，用于“保存设备”后从选择页进入面板，或设备已保存但完成标记写入失败后的恢复。设备 handler 仅在请求携带 completeOnboarding=true 时调用同一完成逻辑。

账号保存不要求再次提交当前密码；有效登录会话就是授权边界。用户名规范化、密码校验和 Argon2id 哈希完成后，在一个 SQLite 事务中同时更新用户名与密码哈希并撤销全部会话，响应清除当前 Cookie，前端回到登录页。

### 4.3 Settings credential projection

GET /api/settings 和设备投影删除 routerosPassword/password 原文字段，只保留 passwordSet: boolean。编辑设备请求增加清晰语义：

- 新设备必须提交非空 password。
- 已有设备的 password 为空表示保留；非空表示替换。
- 连接测试和保存都由后端在需要时合并已有密码。

导出功能基于无密码投影生成，不再先接收明文再遮盖。

## 5. RouterOS verification

### 5.1 Endpoint and input

    POST /api/devices/test-connection
    {
      deviceId?: string,
      scheme: "http" | "https",
      host: string,
      port: number,
      username: string,
      password: string
    }

对已有设备，password 为空时使用服务端现存密码。输入先走与保存相同的 URL 规范化；不把测试草稿写入配置或数据库。

### 5.2 Required and optional probes

共享 routeros.Verify 服务，避免测试逻辑与 Monitor 能力分类漂移。

Required：

- system resource（身份、版本和认证）
- interface list
- IPv4 address list
- IPv6 address list
- DHCP leases
- ARP
- IPv4 connection tracking

Optional warning：

- system health
- ethernet detail
- IPv6 neighbors / IPv6 connection tracking
- simple queues / queue trees / mangle
- routing rules / routing routes（保留 IPv4 route fallback）

Monitor 的 full refresh 同步采用相同 required/optional 分类；否则测试允许保存后，Monitor 仍可能因可选端点失败而永远无快照。

探测整体 deadline 25 秒，底层 HTTP client 保持较短单请求 timeout。错误分类为：地址/协议无效、DNS/连接超时、TLS、认证、必需权限、响应格式；返回面向用户的安全消息，服务端详细日志不得包含 Basic Auth 或密码。

### 5.3 Verification ticket

测试成功后 API 返回：

    {
      verificationToken,
      expiresAt,
      identity: { routerName, version, platform, boardName },
      interfaces: [...],
      cidrCandidates: [...],
      warnings: [...]
    }

服务端内存保存随机 token 的 hash、连接字段指纹、可用接口集合和 15 分钟过期时间，不保存明文密码。连接字段指纹包含规范化 URL、username 和 password 的 SHA-256 派生值。页面刷新或服务重启会失去票据，用户需要重新测试，这是避免持久化临时凭据的有意取舍。

任何连接字段变化都清除浏览器中的 token 和候选。保存时后端重新计算指纹；不匹配、过期或已消费票据返回 409 verification_required。票据只可成功消费一次。

### 5.4 Interface/CIDR validation at save

- 至少一个接口；trim、去重。需要票据时必须出现在票据接口集合中；非连接字段编辑没有票据时，后端即时读取该设备接口列表再验证。
- 至少一个 CIDR；net.ParseCIDR 后保存 canonical network string，trim、去重，IPv4/IPv6 均允许。
- 保存前使用目标凭据对每个所选接口执行一次 monitor-traffic，确认采集权限和接口可采样。
- 先检查规范化 endpoint 在全部现存设备（含 archived）中唯一。
- 所有校验和实时采样成功后才写 YAML。config.Save 改为同目录临时文件、0600 权限、flush/close、原子 rename；失败清理临时文件并保留旧配置。
- YAML 写入失败不消费票据。YAML 成功后消费票据；初始化中的“保存设备”只更新内存配置和 YAML，不调度重启，页面刷新设备列表后可继续添加。“完成设置”请求继续写 onboarding 完成标记并调度重启；完成标记失败时不删除已保存设备，下一次选择页通过 setup/complete(false) 恢复。

编辑仅非连接字段时不要求票据，但仍验证 endpoint 唯一、接口/CIDR 和字段格式。若保存前实时接口权限检查因设备临时离线失败，返回错误且不写配置。

## 6. Frontend architecture

### 6.1 Bootstrap router

App 最外层先加载 /api/bootstrap，渲染四种明确页面：

- AdminSetupPage
- LoginPage
- RouterOSOnboardingPage
- PanelShell

不再以 dashboard === null、RouterOS configured 或任意采集 error 推断 SetupPage。API 返回 401 时清除敏感内存状态并重新加载 bootstrap。

### 6.2 Onboarding wizard

管理员步骤成功后进入设备步骤。设备步骤内部为：

1. Connection form + “测试连接”。
2. 成功身份摘要和 warnings。
3. Interface multi-select + CIDR suggestions/manual entry。
4. 编辑页保留设备列表和“+”新增入口，底部并排显示等尺寸、不同实心背景色的“保存设备”和“完成设置”；前者只持久化并刷新列表、不重启，后者无论当前设备是否已单独保存，都会持久化当前有效草稿、完成初始化并在服务重启恢复后直接进入面板。跳过动作只出现在进入编辑页之前的选择页。

连接测试前隐藏或 disabled 采集字段。连接字段变化立即清除验证结果。密码不写入 localStorage/sessionStorage。

### 6.3 Shared device editor

初始化和设置页复用同一个 DeviceEditor 状态机与 API client，避免设置页绕过测试。已有设备密码输入默认空并显示“已设置；留空保留”。

管理员创建页采用约 24rem 的居中单列表单；账号安全页复用相同的紧凑字段宽度，依次显示用户名、密码、再次输入密码和一个保存按钮。退出登录放在表单之外的独立会话区域。

### 6.4 Empty panel

PanelShell 独立于 Dashboard 是否存在。设备列表为空时：

- 侧栏、设置、账号安全和退出仍渲染。
- 默认内容显示无设备 empty state + 添加按钮。
- 监控页面共享空状态，而非永久 loading 或 setup redirect。

## 7. CLI reset

cmd/rosboard/main.go 把启动逻辑拆成可测试函数：

- 默认 rosboard -config path 启动服务，保持现有 systemd 合同。
- 子命令 rosboard admin reset-password -config path。

CLI 加载相同 config 以定位 data dir，打开 Store，确认管理员存在，通过终端无回显读取两次新密码，执行同一密码验证/哈希服务并撤销全部会话。非 TTY 或输入不一致时失败，不接受命令行明文密码参数。

## 7.1 Full reinitialization

维护页通过 `POST /api/settings/full-reset { confirmed: true }` 提供经过认证的全量重置。浏览器先显示一次不可撤销的确定/取消确认；服务端仍要求 `confirmed=true`，避免误调用空请求。

操作删除配置文件，并在单个 SQLite 事务中清空管理员、会话、初始化状态及所有设备范围的监控表。运行于受 systemd 管理的服务时，重置后关闭当前数据库连接以阻止旧 monitor 在退出前重新写入，再调度进程重启。响应清除 session cookie 和内存验证票据；前端清除 Rosboard 自己的 localStorage/sessionStorage 键。重启后缺失配置文件按全新默认值加载，bootstrap 返回 `needs_admin`。

## 8. Compatibility and migration

- 本任务只验收全新安装，不为已有管理员缺失、旧 YAML 或历史数据库设计迁移 UX。
- 现有多设备 YAML 结构继续作为正式设备配置格式；不主动删除已有 legacy loader，但不为其新增升级保证。
- 认证表创建是幂等 schema initialization，不修改现有监控数据 ownership。
- 设置密码响应合同是有意 breaking change；前端与后端在同一构建中交付。

## 9. Failure and rollback

- 管理员/会话 schema 初始化失败：服务启动失败，不能以无认证模式降级。
- RouterOS test 失败：不写正式状态。
- YAML 保存失败：设备和初始化完成标记均不变。
- YAML 成功但完成标记失败：保留设备，保持 onboarding；向导识别已有有效设备并允许通过 setup/complete(false) 重试完成。
- 重启失败：配置已保存，页面显示维护错误并允许人工重启；不回滚有效配置。
- 远程部署前按项目 gate 备份 binary、config 和 SQLite。回滚必须三者成套恢复，避免新旧 schema/asset 不一致。

## 10. Security notes

- 认证不替代 TLS；HTTP 局域网部署仍可能被同网段窃听，README 继续建议反向代理 HTTPS。
- allowed_cidrs 仍使用实际 socket RemoteAddr；本任务不新增对任意 X-Forwarded-For 的信任。
- RouterOS 凭据因运行时需要仍以 0600 YAML 保存；本任务消除浏览器回传和日志泄漏，不声称静态加密。
- 连接测试属于管理员授权的内网目标访问；限制 scheme 为 HTTP/HTTPS、端口范围 1–65535，并禁止重定向携带 Basic Auth 到其他 host。
