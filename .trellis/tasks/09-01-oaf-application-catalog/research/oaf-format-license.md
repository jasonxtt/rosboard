# OAF format, normalization, and provenance research

审计日期：2026-09-01。这里的 OAF 指 OpenAppFilter（OpenWrt App Filter），不是 rosboard 的运行时组件。

## 1. 研究来源与版本记录

研究使用了上游官方站点、上游 GitHub 仓库、仓库 LICENSE/README 和 release 页面：

- 官方下载页：<https://www.openappfilter.com/en/download.html>
- 上游仓库：<https://github.com/destan19/OpenAppFilter>
- 上游 README：<https://raw.githubusercontent.com/destan19/OpenAppFilter/master/README.md>
- 上游 LICENSE：<https://raw.githubusercontent.com/destan19/OpenAppFilter/refs/heads/master/LICENSE>
- 上游 releases：<https://github.com/destan19/OpenAppFilter/releases/>

本次下载并检查的官方 feature archive 为 `feature3.0_en_20250703.tar.gz`，其中 `feature.cfg` 头部声明：

```text
#version v25.07.03
#format v3.0
#id name:[proto;sport;dport;host url;request;dict;search str;]
```

这只是本次研究快照，不复制到仓库，也不把 feature snapshot version 误写成 OAF 软件 release version。当前 release 页面显示的软件 release 已有 `oaf-v7.0.1`（2026-08-29）；它与 `feature.cfg` 的 `v25.07.03` 是两个独立 provenance 字段。

## 2. 观察到的格式语义

官方页面说明了以下字段和例子：

| OAF 字段 | 观察到的语义 | rosboard 可否直接使用 |
| --- | --- | --- |
| numeric id | 应用 ID；官方说明 ID 唯一，千位可表达类型/类别 | 可作为上游 ID，但必须带 namespace，不能用显示名做主键 |
| name | 展示名称 | 只能是 metadata，不是稳定身份 |
| proto | `tcp` / `udp` | 可规范化为 raw matcher；不能单独证明具体应用 |
| sport/dport | 单端口或范围 | 可识别 transport/service 候选；不能无条件当作品牌应用 |
| host | HTTP/HTTPS host；HTTPS 使用 SNI；示例包含精确域名和域名后缀 | 可规范化成 domain signature，供 attribution；可考虑 RouterOS DNS address-list 投影 |
| url/request | URI 或请求路径匹配 | rosboard 不实现 HTTP payload/DPI；初期不支持 enforcement |
| dict | L7 十六进制字典/负载签名 | 不在 rosboard 实现 OAF/DPI/L7 运行时；初期不支持 |
| search str | 额外搜索/字符串条件 | 没有安全的当前 RouterOS contract；初期不支持 |
| icon | archive 中有 `app_icons/<appid>.png` | 本任务首期 defer；不下载、不 vendor、不做 asset pipeline |

样例还表明：一条应用可以有多个逗号分隔的 signature，例如 WindowsUpdate 同时有 `update.microsoft.com` 和 `windowsupdate.com` 条件；也存在 UDP 443/QUIC、端口范围和 L7 字典样例。解析器必须保留“一个 application 多条 signature”的事实，不能只取第一条或将字段拼成显示文本。

## 3. 规范化规则建议

第一版 Catalog loader 只做最小结构化转换，不实现匹配运行时：

1. 严格读取 format/version/id/name，并拒绝无法解析的 record；坏 record 不能悄悄变成可执行 policy。
2. 应用身份为 `oaf:<numeric-id>`（或明确的 provider namespace + upstream id）。display name 和可选 category 是 metadata；icon 延后。
3. 只有能安全独立解释为 domain exact/suffix 的 signature 才转入 `DomainSignatures`；host 与 request、dict、search 或其他必须共同满足的条件不能摘出 host 使用。
4. loader 可以识别 unsupported 字段并记录/跳过，不建立 generic matcher/capability framework，也不把 unsupported 条件降级成 domain 或 port。
5. 同一 application 的安全 domain signature 做确定性去重；多个逗号分隔的 signature 仍保留为多个候选 domain，跨 application 重叠交给 lookup 的 ambiguous 规则。

归因初期只使用 normalized domain signature：这是从 DNS observation 能得到的唯一直接证据。端口/协议可继续生成 Service fallback，但不覆盖稳定 Application ID。OAF 的 HTTP request、L7 dict 和 search 条件不能因为“看起来像规则”就当作已识别。

## 4. 许可证与再分发边界

上游仓库包含 GPL-2.0 LICENSE，README 说明项目是 OpenWrt 上的 DPI/app identification 软件，并有个人使用、修改、再分发及保留项目/站点引用等额外表述；README 还对公司使用提出联系作者的要求。许可证文件与 README 的法律效果、feature archive/icon 是否与代码同一许可范围，不能由实现者自行推断。

因此本任务的安全边界是：

- 可以实现 rosboard 自己的最小 Catalog schema、loader 和自有测试 fixture；fixture 必须是项目自有的最小测试域名，不复制 OAF feature database 或 icon。
- 不在仓库 vendor 上游 `feature.cfg`、整套应用名称/规则/icon，也不把 OAF 数据编译进 binary；Catalog 数据通过配置的外部 URL/文件获取。
- snapshot status 记录 source、version、loadedAt/lastSuccess、application/domain count、lastError，必要时记录 checksum；不在 runtime 建 license 状态机。
- 任何派生/转换数据是否需要随 rosboard 分发 GPL 文本、NOTICE、源地址和更新机制仍应保留 provenance 说明，但不阻止本任务按“外部数据、不随 binary 分发”的最小方案开始实现。
- refresh 下载失败、内容损坏或解析失败时保留当前 last-good；进程从未得到有效 snapshot 才是 unavailable。

## 5. rosboard 边界

OAF 上游包含 OpenWrt kernel module/service/LuCI 运行时和 DPI 行为。rosboard 不引入这些组件，不做 packet capture、MITM、proxy、kernel module、HTTP payload parser 或 OAF runtime。OAF 在本项目中只提供可读取的应用元数据/安全 domain signature 来源；首期执行能力固定为 domain-only RouterOS DNS address-list。
