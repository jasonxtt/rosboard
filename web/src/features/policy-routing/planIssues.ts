import type { PolicyApiError } from './types'

const errorCodeMessages: Record<string, string> = {
  step_up_required: '请输入当前 rosboard 管理员密码；二次验证只授权当前这一次操作。',
  step_up_failed: '管理员密码不正确',
  step_up_rate_limited: '密码尝试过于频繁，请稍后再试。',
  stale_plan: '实际状态或本地草稿已变化，请重新生成预览后再应用。',
  stale_desired_state: '实际状态或本地草稿已变化，请重新生成预览后再应用。',
  plan_expired: '预览已过期（默认 15 分钟），请重新生成。',
  plan_replayed: '该计划已经被应用过，不能重复执行。',
  job_conflict: '该设备已有正在执行的策略任务，请等待其完成或先取消。',
  plan_blocked: '计划存在阻止项，请根据下方阻止原因调整草稿后重新预览。',
  acknowledgements_required: '存在必须确认的风险项，请逐项勾选后再应用。',
  policy_access_required: '尚未配置策略管理账号，请先完成账号接入。',
  policy_manager_unavailable: '策略运行时暂不可用，请稍后重试；若持续存在请检查策略账号与设备连通性。',
  policy_runtime_unavailable: '策略运行时暂不可用，请稍后重试；若持续存在请检查策略账号与设备连通性。',
  policy_discovery_unavailable: '策略管理账号无效或 RouterOS 发现不可用，请先修复账号后再生成差异预览。',
  routeros_policy_write_capability_unverified: 'RouterOS 写入能力未验证',
  routeros_policy_access: 'RouterOS 策略访问失败',
  invalid_gateway: '网关地址无效',
  source_upstream_failed: '下载来源失败，请确认 URL 可公开访问且内容为 Clash YAML。',
  preview_expired: '解析预览已过期（15 分钟），请重新执行预览。',
  preview_mismatch: '预览与当前来源不匹配，请重新预览后再保存。',
  source_preview_required: '来源内容变化后必须先执行解析预览。',
  upload_too_large: '上传文件超过 5 MiB 限制。',
  device_disabled: '设备已禁用',
  device_archived: '设备已归档',
  policy_storage_unavailable: '策略存储不可用',
  network_error: '网络连接失败',
  unknown_error: '未知错误',
  read_only_plan: '该计划仅预览、不可应用',
  invalid_source_preview: '域名列表预览无效',
  invalid_upload: '上传内容无效',
  source_preview_unavailable: '域名列表预览不可用',
  revision_stale: '配置已被修改，请刷新后重试',
  device_required: '缺少设备参数',
  device_not_found: '设备不存在',
  invalid_body: '请求内容无效',
  invalid_access: '访问配置无效',
  invalid_url: 'URL 格式无效',
  invalid_egress: '出口配置无效',
  invalid_source: '域名列表配置无效',
  invalid_revision: '修订版本无效',
  invalid_lan_scope: '局域网范围无效',
  save_failed: '保存失败',
  fetch_failed: '获取失败',
  parse_failed: '解析失败',
  egress_not_found: '出口不存在',
  source_not_found: '域名列表不存在',
  source_unavailable: '域名列表不可用',
  runtime_unavailable: '运行时不可用',
  apply_failed: '应用失败',
  routeros_not_configured: 'RouterOS 未配置',
  provisioning_failed: '配置脚本生成失败',
  credential_verify_failed: '凭证验证失败',
  not_configured: '尚未配置',
}

export function policyErrorCodeMessage(code: string | undefined): string | null {
  if (!code) return null
  return errorCodeMessages[code] ?? null
}

export function policyErrorDescription(error: PolicyApiError): string {
  const fromCode = policyErrorCodeMessage(error.code)
  return fromCode ?? error.message
}
