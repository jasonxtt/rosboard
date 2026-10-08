import type { Draft, Item } from './types'
// getRandomValues also works on HTTP LAN panels, where randomUUID may be absent.
export function newRequestId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) =>
    value.toString(16).padStart(2, '0'),
  ).join('')
}
export function newDraft(): Draft {
  return {
    draftId: newRequestId(),
    existingId: '',
    name: '',
    image: '',
    network: {
      veth: '',
      bridge: '',
      address: '',
      gateway: '',
      address6: '',
      gateway6: '',
      mac: '',
    },
    rootDir: '',
    command: '',
    entrypoint: '',
    user: '',
    workdir: '',
    env: [],
    mounts: [],
    ports: [],
    memoryHigh: '',
    memoryMax: '',
    cpuList: '',
    startAfterCreate: true,
    startOnBoot: true,
    logging: true,
    restartPolicy: 'no',
    health: {
      mode: 'inherit',
      command: '',
      interval: '',
      timeout: '',
      retries: '',
      startPeriod: '',
    },
  }
}
export function editDraft(item: Item): Draft {
  return structuredClone({ ...item.config, existingId: item.id })
}
export function normalizedImage(image: string): string {
  image = image.trim()
  if (!image || image.includes('@')) return image
  return image.slice(image.lastIndexOf('/') + 1).includes(':')
    ? image
    : image + ':latest'
}
export const statusLabels: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  starting: '启动中',
  stopping: '停止中',
  downloading: '下载中',
  extracting: '解压中',
  error: '异常',
  unknown: '未知',
}

/** RouterOS reports byte strings; simulation also uses human-readable units. */
export function memoryBytes(value: string): number {
  const n = Number.parseFloat(value)
  if (!Number.isFinite(n)) return 0
  const unit = value
    .trim()
    .replace(/^[0-9.]+\s*/, '')
    .toLowerCase()
  const scale: Record<string, number> = {
    kib: 2 ** 10,
    mib: 2 ** 20,
    gib: 2 ** 30,
    tib: 2 ** 40,
    kb: 1e3,
    mb: 1e6,
    gb: 1e9,
    tb: 1e12,
  }
  return n * (scale[unit] ?? 1)
}
