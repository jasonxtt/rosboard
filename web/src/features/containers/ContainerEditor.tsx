import {
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEventHandler,
  type ReactNode,
} from 'react'
import { errorMessage } from '../../lib/api'
import { resolveDraft } from './api'
import { normalizedImage } from './drafts'
import { DirectoryPicker } from './DirectoryPicker'
import { ImageUpload } from './ImageUpload'
import type { Draft, Item, Resolution, Snapshot } from './types'

function Field({
  label,
  value,
  onChange,
  hint,
  error,
  required = false,
  disabled = false,
  type = 'text',
  multiline = false,
  onClick,
  onKeyDown,
  controls,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  hint?: string
  error?: string
  required?: boolean
  disabled?: boolean
  type?: string
  multiline?: boolean
  onClick?: () => void
  onKeyDown?: KeyboardEventHandler<HTMLInputElement>
  controls?: string
}) {
  return (
    <label className="ct-field">
      <span>
        {label}
        {required && <em aria-label="必填"> *</em>}
      </span>
      {multiline ? (
        <textarea
          rows={2}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          aria-invalid={!!error}
        />
      ) : (
        <input
          type={type}
          onClick={onClick}
          onKeyDown={onKeyDown}
          aria-controls={controls}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          aria-invalid={!!error}
        />
      )}
      {hint && <small>{hint}</small>}
      {error && <strong role="alert">{error}</strong>}
    </label>
  )
}
function Section({
  title,
  number,
  children,
  description,
}: {
  title: string
  number: string
  children: ReactNode
  description: string
}) {
  return (
    <section className="ct-card ct-section">
      <header>
        <span className="ct-section-number">{number}</span>
        <div>
          <h3>{title}</h3>
          <p>{description}</p>
        </div>
      </header>
      <div className="ct-section-body">{children}</div>
    </section>
  )
}
export function ContainerEditor({
  deviceId,
  snapshot,
  initial,
  item,
  busy,
  onSubmit,
  onClose,
}: {
  deviceId: string
  snapshot: Snapshot
  initial: Draft
  item: Item | null
  busy: boolean
  onSubmit: (draft: Draft) => Promise<void>
  onClose: () => void
}) {
  const [draft, setDraft] = useState(() => structuredClone(initial)),
    [resolution, setResolution] = useState<Resolution | null>(null),
    [error, setError] = useState(''),
    [validating, setValidating] = useState(false),
    [submitted, setSubmitted] = useState(false),
    [uploading, setUploading] = useState(false),
    [picker, setPicker] = useState<'root' | number | null>(null),
    [advancedNetwork, setAdvancedNetwork] = useState(false),
    [advancedStartup, setAdvancedStartup] = useState(false)
  const advancedNetworkID = useId(),
    advancedStartupID = useId(),
    directoryID = useId()
  const lifetime = useRef<{ active: boolean; submit?: AbortController }>({
    active: true,
  })
  useEffect(() => {
    const state = lifetime.current
    state.active = true
    return () => {
      state.active = false
      state.submit?.abort()
    }
  }, [])
  const sequence = useRef(0),
    locked = !!item?.sharedVeth.length
  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }))
  const net = (key: keyof Draft['network'], value: string) =>
    patch('network', { ...draft.network, [key]: value })
  const health = (key: keyof Draft['health'], value: string) =>
    patch('health', { ...draft.health, [key]: value })
  const issue = (key: string) =>
    submitted ? resolution?.errors[key] : undefined
  useEffect(() => {
    const current = ++sequence.current
    const controller = new AbortController()
    let cancelled = false
    const timer = setTimeout(() => {
      void resolveDraft(deviceId, draft, controller.signal)
        .then((r) => {
          if (!cancelled && sequence.current === current) {
            setResolution(r)
            setError('')
          }
        })
        .catch((e) => {
          if (!cancelled && sequence.current === current)
            setError(errorMessage(e))
        })
    }, 350)
    return () => {
      clearTimeout(timer)
      cancelled = true
      controller.abort()
    }
  }, [deviceId, draft])
  const submit = async () => {
    const current = sequence.current
    const controller = new AbortController()
    lifetime.current.submit = controller
    setValidating(true)
    setSubmitted(true)
    setError('')
    try {
      const r = await resolveDraft(deviceId, draft, controller.signal)
      if (!lifetime.current.active || sequence.current !== current) return
      setResolution(r)
      if (
        ['network.address6', 'network.gateway6', 'network.mac'].some(
          (key) => r.errors[key],
        )
      )
        setAdvancedNetwork(true)
      if (
        ['command', 'entrypoint', 'user', 'workdir'].some(
          (key) => r.errors[key],
        )
      )
        setAdvancedStartup(true)
      if (Object.keys(r.errors).length === 0) await onSubmit(r.effective)
    } catch (e) {
      if (lifetime.current.active) setError(errorMessage(e))
    } finally {
      if (lifetime.current.active) setValidating(false)
    }
  }
  const directoryPicker =
    picker !== null ? (
      <DirectoryPicker
        key={String(picker)}
        id={directoryID}
        deviceId={deviceId}
        writable={snapshot.capabilities.writes}
        purpose={picker === 'root' ? '容器运行目录' : `挂载源 ${picker + 1}`}
        allowFiles={picker !== 'root'}
        onClose={() => setPicker(null)}
        onSelect={(path) => {
          if (picker === 'root') patch('rootDir', path)
          else
            patch(
              'mounts',
              draft.mounts.map((m, i) =>
                i === picker ? { ...m, source: path } : m,
              ),
            )
          setPicker(null)
        }}
      />
    ) : null

  return (
    <form
      className="ct-editor"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
      noValidate
    >
      <div className="ct-editor-heading">
        <div>
          <span className="ct-eyebrow">
            {item ? 'CONTAINER CONFIGURATION' : 'NEW CONTAINER'}
          </span>
          <h2>{item ? '编辑容器' : '创建容器'}</h2>
          <p>所有配置在这一页。带 * 的项目需填写，其余项目保留默认值。</p>
        </div>
        <button
          type="button"
          onClick={onClose}
          disabled={busy || validating || uploading}
        >
          返回列表
        </button>
      </div>
      {item && (
        <div className="ct-notice">
          原有值已保留。环境变量列表：{item.envLists.join(', ') || '无'}
          ；挂载列表：{item.mountLists.join(', ') || '无'}。
          {item.ownership === 'unmanaged' &&
            '此容器尚未接管，当前只可预览配置。'}
        </div>
      )}
      <fieldset
        className="ct-form-grid"
        disabled={busy || validating || uploading}
      >
        <Section
          title="镜像"
          number="01"
          description="从仓库拉取或上传本地镜像归档，继承镜像默认配置。"
        >
          <label className="ct-field">
            <span>镜像来源</span>
            <select
              value={draft.imageSource}
              onChange={(e) => patch('imageSource', e.target.value)}
            >
              <option value="registry">镜像仓库</option>
              <option value="archive">本地上传</option>
            </select>
          </label>
          {draft.imageSource === 'archive' ? (
            <>
              <ImageUpload
                deviceId={deviceId}
                enabled={snapshot.capabilities.writes}
                archiveFile={draft.archiveFile}
                onBusy={setUploading}
                onUploaded={(archive) =>
                  setDraft((d) => ({
                    ...d,
                    image: archive.reference,
                    archiveId: archive.id,
                    archiveFile: archive.remotePath,
                  }))
                }
              />
              {issue('archiveId') && (
                <strong role="alert">{issue('archiveId')}</strong>
              )}
              {issue('imageSource') && (
                <strong role="alert">{issue('imageSource')}</strong>
              )}
            </>
          ) : (
            <Field
              label="镜像引用"
              value={draft.image}
              onChange={(v) => patch('image', v)}
              required
              error={issue('image')}
              hint={
                normalizedImage(draft.image) ||
                '例如 ghcr.io/example/service:1.0'
              }
            />
          )}
          <Field
            label="容器名称"
            value={draft.name}
            onChange={(v) => patch('name', v)}
            error={issue('name')}
            hint={
              resolution?.effective.name
                ? `最终名称：${resolution.effective.name}`
                : '留空按镜像名称生成唯一名称'
            }
          />
        </Section>
        <Section
          title="网络"
          number="02"
          description="专属 VETH 接入已有 bridge。通过容器 IP:应用端口访问，沿用现有路由与防火墙。"
        >
          {locked && (
            <p className="ct-notice">
              VETH 与 {item?.sharedVeth.join(', ')} 共享，网络参数已锁定。
            </p>
          )}
          <Field
            label="VETH 名称"
            value={draft.network.veth}
            onChange={(v) => net('veth', v)}
            required
            disabled={locked}
            error={issue('network.veth')}
          />
          <label className="ct-field">
            <span>
              已有 bridge <em>*</em>
            </span>
            <select
              value={draft.network.bridge}
              onChange={(e) => net('bridge', e.target.value)}
              disabled={locked}
              aria-invalid={!!issue('network.bridge')}
            >
              <option value="">请选择 bridge</option>
              {snapshot.options.bridges.map((b) => (
                <option key={b}>{b}</option>
              ))}
            </select>
            {issue('network.bridge') && (
              <strong role="alert">{issue('network.bridge')}</strong>
            )}
          </label>
          <div className="ct-pair">
            <Field
              label="静态 IPv4 / 掩码"
              value={draft.network.address}
              onChange={(v) => net('address', v)}
              required
              disabled={locked}
              error={issue('network.address')}
              hint="例如 172.20.0.2/24"
            />
            <Field
              label="IPv4 网关"
              value={draft.network.gateway}
              onChange={(v) => net('gateway', v)}
              required
              disabled={locked}
              error={issue('network.gateway')}
            />
          </div>
          <button
            type="button"
            className="ct-advanced-toggle"
            aria-label="网络高级设置"
            aria-expanded={advancedNetwork}
            aria-controls={advancedNetworkID}
            onClick={() => setAdvancedNetwork((open) => !open)}
          >
            <span aria-hidden="true">{advancedNetwork ? '▴' : '▾'}</span>
            高级设置
            {(draft.network.address6 ||
              draft.network.gateway6 ||
              draft.network.mac) && <small>已配置</small>}
          </button>
          {advancedNetwork && (
            <div
              className="ct-network-advanced ct-advanced-fields"
              id={advancedNetworkID}
            >
              <div className="ct-pair">
                <Field
                  label="IPv6 / 掩码"
                  value={draft.network.address6}
                  onChange={(v) => net('address6', v)}
                  disabled={locked}
                  error={issue('network.address6')}
                />
                <Field
                  label="IPv6 网关"
                  value={draft.network.gateway6}
                  onChange={(v) => net('gateway6', v)}
                  disabled={locked}
                  error={issue('network.gateway6')}
                />
              </div>
              <Field
                label="自定义 MAC"
                value={draft.network.mac}
                onChange={(v) => net('mac', v)}
                disabled={locked}
                error={issue('network.mac')}
                hint="留空由 RouterOS 生成"
              />
            </div>
          )}
        </Section>
        <Section
          title="存储与挂载"
          number="03"
          description="容器运行目录与配置、数据挂载可共用父目录，分别使用子目录。"
        >
          <Field
            label="容器运行目录（root-dir）"
            value={draft.rootDir}
            onChange={(v) => patch('rootDir', v)}
            onClick={() => setPicker('root')}
            controls={picker === 'root' ? directoryID : undefined}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setPicker('root')
              } else if (event.key === 'Escape' && picker === 'root') {
                event.preventDefault()
                setPicker(null)
              }
            }}
            error={issue('rootDir')}
            hint={
              resolution?.effective.rootDir
                ? `最终目录：${resolution.effective.rootDir}`
                : '自动选择空闲空间最大的可用磁盘'
            }
          />
          <small>点击输入框或按 ↓ 浏览 Files，也可直接输入路径。</small>
          {picker === 'root' && directoryPicker}
          <p className="ct-storage-hint">
            可放在同一父目录下：<code>nginx/rootdir/</code> 用作运行目录，
            <code>nginx/data/config/</code>{' '}
            用作配置挂载源。挂载源应位于运行目录之外，目录名称可自定义。
          </p>
          <small>
            可用磁盘：
            {snapshot.options.disks
              .filter((d) => d.writable)
              .map(
                (d) =>
                  `${d.name} · ${(d.freeBytes / 2 ** 30).toFixed(1)} GiB 空闲`,
              )
              .join(' / ') || '无'}
          </small>
          {draft.mounts.map((m, i) => (
            <div className="ct-repeat" key={i}>
              <Field
                label="主机源目录"
                value={m.source}
                onChange={(v) =>
                  patch(
                    'mounts',
                    draft.mounts.map((x, j) =>
                      j === i ? { ...x, source: v } : x,
                    ),
                  )
                }
              />
              <button
                type="button"
                className="ct-add"
                onClick={() => setPicker(i)}
              >
                浏览挂载源 {i + 1}
              </button>
              <Field
                label="容器目标目录"
                value={m.target}
                onChange={(v) =>
                  patch(
                    'mounts',
                    draft.mounts.map((x, j) =>
                      j === i ? { ...x, target: v } : x,
                    ),
                  )
                }
              />
              <label className="ct-check">
                <input
                  type="checkbox"
                  checked={m.readOnly}
                  onChange={(e) =>
                    patch(
                      'mounts',
                      draft.mounts.map((x, j) =>
                        j === i ? { ...x, readOnly: e.target.checked } : x,
                      ),
                    )
                  }
                />
                只读
              </label>
              <button
                type="button"
                onClick={() => {
                  setPicker(null)
                  patch(
                    'mounts',
                    draft.mounts.filter((_, j) => i !== j),
                  )
                }}
              >
                移除挂载 {i + 1}
              </button>
              {issue(`mounts.${i}`) && (
                <strong role="alert">{issue(`mounts.${i}`)}</strong>
              )}
            </div>
          ))}
          <button
            type="button"
            className="ct-add"
            onClick={() =>
              patch('mounts', [
                ...draft.mounts,
                { source: '', target: '', readOnly: false },
              ])
            }
          >
            ＋ 添加挂载
          </button>
          {typeof picker === 'number' && directoryPicker}
        </Section>
        <Section
          title="环境变量"
          number="04"
          description="只添加显式覆盖，值中的空格、引号和特殊字符原样保留。"
        >
          {draft.env.length === 0 && (
            <p className="ct-empty-inline">继承镜像中的环境变量</p>
          )}
          {draft.env.map((v, i) => (
            <div className="ct-repeat" key={i}>
              <div className="ct-env-row">
                <Field
                  label="变量名"
                  value={v.key}
                  onChange={(value) =>
                    patch(
                      'env',
                      draft.env.map((x, j) =>
                        j === i ? { ...x, key: value } : x,
                      ),
                    )
                  }
                />
                <Field
                  label="变量值"
                  multiline
                  value={v.value}
                  onChange={(value) =>
                    patch(
                      'env',
                      draft.env.map((x, j) => (j === i ? { ...x, value } : x)),
                    )
                  }
                />
                <button
                  type="button"
                  className="ct-icon-delete"
                  aria-label={`删除变量 ${i + 1}`}
                  title={`删除变量 ${i + 1}`}
                  onClick={() =>
                    patch(
                      'env',
                      draft.env.filter((_, j) => i !== j),
                    )
                  }
                >
                  <span aria-hidden="true">🗑️</span>
                </button>
              </div>
              {issue(`env.${i}`) && (
                <strong role="alert">{issue(`env.${i}`)}</strong>
              )}
            </div>
          ))}
          <button
            type="button"
            className="ct-add"
            onClick={() => patch('env', [...draft.env, { key: '', value: '' }])}
          >
            ＋ 添加变量
          </button>
        </Section>
        <Section
          title="启动配置"
          number="05"
          description="设置何时启动、自动重启和日志记录。通常无需修改高级设置。"
        >
          {!item && (
            <label className="ct-check">
              <input
                type="checkbox"
                checked={draft.startAfterCreate}
                onChange={(e) => patch('startAfterCreate', e.target.checked)}
              />
              创建后启动
            </label>
          )}
          <label className="ct-check">
            <input
              type="checkbox"
              checked={draft.startOnBoot}
              onChange={(e) => patch('startOnBoot', e.target.checked)}
            />
            开机自动启动
          </label>
          <label className="ct-check">
            <input
              type="checkbox"
              checked={draft.logging}
              onChange={(e) => patch('logging', e.target.checked)}
            />
            记录容器日志
          </label>
          <label className="ct-field">
            <span>自动重启策略</span>
            <select
              value={draft.restartPolicy || 'no'}
              onChange={(e) => patch('restartPolicy', e.target.value)}
            >
              <option value="no">不自动重启（默认）</option>
              <option value="on-failure">失败时重启</option>
              <option value="always">总是重启</option>
            </select>
          </label>
          <button
            type="button"
            className="ct-advanced-toggle"
            aria-label="启动高级设置"
            aria-expanded={advancedStartup}
            aria-controls={advancedStartupID}
            onClick={() => setAdvancedStartup((open) => !open)}
          >
            <span aria-hidden="true">{advancedStartup ? '▴' : '▾'}</span>
            高级设置
            {(draft.command ||
              draft.entrypoint ||
              draft.user ||
              draft.workdir) && <small>已配置</small>}
          </button>
          {advancedStartup && (
            <div
              className="ct-startup-advanced ct-advanced-fields"
              id={advancedStartupID}
            >
              <small>
                留空使用镜像自带的启动设置。编辑已有容器时保留原值。
              </small>
              <Field
                label="命令 CMD"
                value={draft.command}
                onChange={(v) => patch('command', v)}
                error={issue('command')}
                hint={item?.imageDefaults.cmd || '继承镜像命令'}
              />
              <Field
                label="入口 ENTRYPOINT"
                value={draft.entrypoint}
                onChange={(v) => patch('entrypoint', v)}
                error={issue('entrypoint')}
                hint={item?.imageDefaults.entrypoint || '继承镜像入口'}
              />
              <div className="ct-pair">
                <Field
                  label="用户"
                  value={draft.user}
                  onChange={(v) => patch('user', v)}
                  error={issue('user')}
                  hint={item?.imageDefaults.user || '继承镜像用户'}
                />
                <Field
                  label="工作目录"
                  value={draft.workdir}
                  onChange={(v) => patch('workdir', v)}
                  error={issue('workdir')}
                  hint={item?.imageDefaults.workdir || '继承镜像工作目录'}
                />
              </div>
            </div>
          )}
        </Section>
        <Section
          title="资源限制"
          number="06"
          description="留空继承 RouterOS 全局内存设置与 CPU 默认值。"
        >
          <div className="ct-pair">
            <Field
              label="内存 high"
              value={draft.memoryHigh}
              onChange={(v) => patch('memoryHigh', v)}
              error={issue('memoryHigh')}
              hint={`全局：${snapshot.options.memoryHigh || 'unlimited'}`}
            />
            <Field
              label="内存 max"
              value={draft.memoryMax}
              onChange={(v) => patch('memoryMax', v)}
              error={issue('memoryMax')}
              hint={`全局：${snapshot.options.memoryMax || 'unlimited'}`}
            />
          </div>
          <Field
            label="CPU 编号列表"
            value={draft.cpuList}
            onChange={(v) => patch('cpuList', v)}
            error={issue('cpuList')}
            hint="留空使用默认；例如 0,1"
          />
        </Section>
        <Section
          title="健康检查"
          number="07"
          description="定时在容器内执行命令，确认服务能否正常响应。通常保留镜像设置即可。"
        >
          <label className="ct-field">
            <span>检查方式</span>
            <select
              value={draft.health.mode}
              onChange={(e) => health('mode', e.target.value)}
            >
              <option value="inherit">使用镜像自带的检查（推荐）</option>
              <option value="override">自定义检查</option>
            </select>
            {issue('health.mode') && (
              <strong role="alert">{issue('health.mode')}</strong>
            )}
          </label>
          <p className="ct-health-hint">
            例如：定时访问 Nginx
            的网页，确认服务有响应。镜像未提供检查时，默认不会检查，也不影响容器启动。
            检查异常后的停机、重启或通知需要另行配置。
          </p>
          <small>
            下列设置仅在“自定义检查”时生效；留空沿用镜像或 RouterOS 默认值。
          </small>
          <Field
            label="检查命令"
            value={draft.health.command}
            onChange={(v) => health('command', v)}
            disabled={draft.health.mode !== 'override'}
            error={issue('health.command')}
            hint={
              item?.imageDefaults['healthcheck-cmd'] ||
              '例如 curl -f http://127.0.0.1:80/；127.0.0.1 指容器自身，端口按应用修改，镜像内需有 curl。'
            }
          />
          <div className="ct-pair">
            <Field
              label="检查间隔"
              error={issue('health.interval')}
              value={draft.health.interval}
              onChange={(v) => health('interval', v)}
              disabled={draft.health.mode !== 'override'}
              hint="每隔多久检查一次，例如 30s（30 秒）"
            />
            <Field
              label="单次检查超时"
              error={issue('health.timeout')}
              value={draft.health.timeout}
              onChange={(v) => health('timeout', v)}
              disabled={draft.health.mode !== 'override'}
              hint="一次检查最多等待多久，例如 5s（5 秒）"
            />
          </div>
          <div className="ct-pair">
            <Field
              label="连续失败次数"
              value={draft.health.retries}
              onChange={(v) => health('retries', v)}
              disabled={draft.health.mode !== 'override'}
              error={issue('health.retries')}
              hint="连续失败多少次才标记异常，例如 3"
            />
            <Field
              label="启动准备时间"
              error={issue('health.startPeriod')}
              value={draft.health.startPeriod}
              onChange={(v) => health('startPeriod', v)}
              disabled={draft.health.mode !== 'override'}
              hint="给服务启动留出时间，例如 20s（20 秒）；期间的失败不计入连续失败次数"
            />
          </div>
        </Section>
      </fieldset>
      <footer className="ct-card ct-resolution">
        <h3>最终采用的配置</h3>
        <div className="ct-defaults">
          {resolution?.defaults.map((v) => <span key={v}>{v}</span>) || (
            <span>正在解析默认值…</span>
          )}
        </div>
        {error && <p role="alert">{error}</p>}
        {submitted &&
          resolution &&
          Object.keys(resolution.errors).length > 0 && (
            <p role="alert">请修正标出的配置，已填写内容会保留。</p>
          )}
        <div className="ct-actions">
          <small>
            {snapshot.capabilities.writes
              ? '模拟服务 · 所有操作只影响模拟数据'
              : '只读模式 · 可校验配置，真实写入尚未启用'}
          </small>
          <button
            className="ct-primary"
            type="submit"
            disabled={busy || validating || uploading}
          >
            {validating
              ? '正在校验…'
              : snapshot.capabilities.writes &&
                  (!item || item.ownership === 'managed')
                ? item
                  ? '保存配置'
                  : '创建容器'
                : '校验配置（只读）'}
          </button>
        </div>
      </footer>
    </form>
  )
}
