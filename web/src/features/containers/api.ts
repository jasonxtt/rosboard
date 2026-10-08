import {
  ApiError,
  apiGet,
  apiPost,
  apiPostForm,
  safeObject,
  safeArray,
  safeString,
  safeNumber,
  safeBoolean,
  scoped,
} from '../../lib/api'
import type {
  ImageArchive,
  DirectoryListing,
  Network,
  Draft,
  Item,
  Snapshot,
  Options,
  Capabilities,
  Resolution,
  Job,
  Action,
  ContainerLog,
} from './types'
const strings = (v: unknown) =>
  safeArray<unknown>(v).filter((x): x is string => typeof x === 'string')
const record = (v: unknown) =>
  Object.fromEntries(
    Object.entries(safeObject(v)).filter(
      (entry): entry is [string, string] => typeof entry[1] === 'string',
    ),
  )
export function parseNetwork(v: unknown): Network {
  const o = safeObject(v)
  return {
    veth: safeString(o.veth),
    bridge: safeString(o.bridge),
    address: safeString(o.address),
    gateway: safeString(o.gateway),
    address6: safeString(o.address6),
    gateway6: safeString(o.gateway6),
    mac: safeString(o.mac),
  }
}
export function parseDraft(v: unknown): Draft {
  const o = safeObject(v),
    h = safeObject(o.health)
  return {
    draftId: safeString(o.draftId),
    existingId: safeString(o.existingId),
    name: safeString(o.name),
    image: safeString(o.image),
    imageSource: safeString(o.imageSource) || 'registry',
    archiveId: safeString(o.archiveId),
    archiveFile: safeString(o.archiveFile),
    network: parseNetwork(o.network),
    rootDir: safeString(o.rootDir),
    command: safeString(o.command),
    entrypoint: safeString(o.entrypoint),
    user: safeString(o.user),
    workdir: safeString(o.workdir),
    memoryHigh: safeString(o.memoryHigh),
    memoryMax: safeString(o.memoryMax),
    cpuList: safeString(o.cpuList),
    startAfterCreate: safeBoolean(o.startAfterCreate),
    startOnBoot: safeBoolean(o.startOnBoot),
    logging: safeBoolean(o.logging),
    restartPolicy: safeString(o.restartPolicy),
    env: safeArray<unknown>(o.env).map((v) => {
      const e = safeObject(v)
      return { key: safeString(e.key), value: safeString(e.value) }
    }),
    mounts: safeArray<unknown>(o.mounts).map((v) => {
      const m = safeObject(v)
      return {
        source: safeString(m.source),
        target: safeString(m.target),
        readOnly: safeBoolean(m.readOnly),
        mode: safeString(m.mode),
      }
    }),
    health: {
      mode: safeString(h.mode),
      command: safeString(h.command),
      interval: safeString(h.interval),
      timeout: safeString(h.timeout),
      retries: safeString(h.retries),
      startPeriod: safeString(h.startPeriod),
    },
  }
}
export function parseItem(v: unknown): Item {
  const o = safeObject(v)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    status: safeString(o.status),
    image: safeString(o.image),
    network: parseNetwork(o.network),
    cpu: safeString(o.cpu),
    memory: safeString(o.memory),
    startOnBoot: safeBoolean(o.startOnBoot),
    ownership: safeString(o.ownership) || 'unmanaged',
    sharedVeth: strings(o.sharedVeth),
    envLists: strings(o.envLists),
    mountLists: strings(o.mountLists),
    config: parseDraft(o.config),
    imageDefaults: record(o.imageDefaults),
  }
}
function parseOptions(v: unknown): Options {
  const o = safeObject(v)
  return {
    architecture: safeString(o.architecture),
    archives: safeArray<unknown>(o.archives).map(parseArchive),
    bridges: strings(o.bridges),
    interfaces: strings(o.interfaces),
    usedIPs: strings(o.usedIPs),
    disks: safeArray<unknown>(o.disks).map((v) => {
      const d = safeObject(v)
      return {
        name: safeString(d.name),
        freeBytes: safeNumber(d.freeBytes),
        writable: safeBoolean(d.writable),
      }
    }),
    memoryHigh: safeString(o.memoryHigh),
    memoryMax: safeString(o.memoryMax),
  }
}
function parseCapabilities(v: unknown): Capabilities {
  const o = safeObject(v)
  return {
    supported: safeBoolean(o.supported),
    writes:
      safeBoolean(o.writes) &&
      safeBoolean(o.supported) &&
      o.mode === 'simulation',
    mode: safeString(o.mode) || 'read-only',
    version: safeString(o.version),
    logs: safeBoolean(o.logs),
    fields: strings(o.fields),
    warnings: strings(o.warnings),
  }
}
export function parseSnapshot(v: unknown): Snapshot {
  const o = safeObject(v)
  return {
    activeJob: o.activeJob ? parseJob(o.activeJob) : null,
    items: safeArray<unknown>(o.items).map(parseItem),
    options: parseOptions(o.options),
    capabilities: parseCapabilities(o.capabilities),
  }
}
export function parseResolution(v: unknown): Resolution {
  const o = safeObject(v)
  if (
    !o.effective ||
    typeof o.effective !== 'object' ||
    !o.errors ||
    typeof o.errors !== 'object' ||
    Array.isArray(o.errors)
  )
    throw new ApiError('配置解析响应无效', 200, 'invalid_response')
  return {
    effective: parseDraft(o.effective),
    errors: record(o.errors),
    defaults: strings(o.defaults),
    containerFields: record(o.containerFields),
  }
}
export function parseJob(v: unknown): Job {
  const o = safeObject(v)
  return {
    id: safeString(o.id),
    deviceId: safeString(o.deviceId),
    action: safeString(o.action),
    targetId: safeString(o.targetId),
    state: safeString(o.state),
    phase: safeString(o.phase),
    progress: Math.max(0, Math.min(100, safeNumber(o.progress))),
    error: safeString(o.error),
    retained: strings(o.retained),
  }
}
const endpoint = (device: string, path = '') =>
  scoped('/api/containers' + path, device)
export const fetchContainers = (device: string, signal?: AbortSignal) =>
  apiGet(endpoint(device), parseSnapshot, signal)
export const fetchContainer = (
  device: string,
  id: string,
  signal?: AbortSignal,
) => apiGet(endpoint(device, '/' + encodeURIComponent(id)), parseItem, signal)
export const resolveDraft = (
  device: string,
  draft: Draft,
  signal?: AbortSignal,
) => apiPost(endpoint(device, '/resolve'), draft, parseResolution, signal)
export const performAction = (
  device: string,
  action: Action,
  targetId: string,
  requestId: string,
  draft?: Draft,
  scenario = 'success',
) =>
  apiPost(
    endpoint(device, '/actions'),
    { action, targetId, requestId, draft, scenario },
    parseJob,
  )
export const fetchJob = (device: string, id: string, signal?: AbortSignal) =>
  apiGet(endpoint(device, '/jobs/' + encodeURIComponent(id)), parseJob, signal)
export const recoverJob = (device: string, id: string) =>
  apiPost(
    endpoint(device, '/jobs/' + encodeURIComponent(id) + '/recover'),
    {},
    parseJob,
  )
export const fetchLogs = (device: string, id: string, signal?: AbortSignal) =>
  apiGet(
    endpoint(device, '/' + encodeURIComponent(id) + '/logs'),
    (v) =>
      safeArray<unknown>(safeObject(v).logs).map((v): ContainerLog => {
        const o = safeObject(v)
        return {
          id: safeString(o.id),
          time: safeString(o.time),
          message: safeString(o.message),
        }
      }),
    signal,
  )

export function parseArchive(value: unknown): ImageArchive {
  const o = safeObject(value)
  return {
    id: safeString(o.id),
    name: safeString(o.name),
    reference: safeString(o.reference),
    architecture: safeString(o.architecture),
    bytes: safeNumber(o.bytes),
    sha256: safeString(o.sha256),
    remotePath: safeString(o.remotePath),
  }
}
export const uploadImage = (
  device: string,
  file: File,
  signal?: AbortSignal,
) => {
  const form = new FormData()
  form.append('file', file)
  return apiPostForm(
    endpoint(device, '/images/upload'),
    form,
    (value) => {
      const archive = parseArchive(value)
      if (
        !archive.id ||
        !archive.reference ||
        !archive.remotePath.startsWith('/') ||
        archive.bytes <= 0 ||
        !/^[a-f0-9]{64}$/.test(archive.sha256)
      )
        throw new ApiError('镜像校验响应无效', 200, 'invalid_response')
      return archive
    },
    signal,
  )
}
export const fetchDirectories = (
  device: string,
  path: string,
  signal?: AbortSignal,
) =>
  apiGet(
    endpoint(device, '/directories') + '&path=' + encodeURIComponent(path),
    (value): DirectoryListing => {
      const o = safeObject(value)
      if (
        typeof o.path !== 'string' ||
        !o.path.startsWith('/') ||
        !Array.isArray(o.entries)
      )
        throw new ApiError('目录响应无效', 200, 'invalid_response')
      return {
        path: o.path,
        entries: o.entries.map((value) => {
          const e = safeObject(value)
          return {
            name: safeString(e.name),
            path: safeString(e.path),
            directory: safeBoolean(e.directory),
            bytes: safeNumber(e.bytes),
          }
        }),
      }
    },
    signal,
  )
export const createDirectory = (
  device: string,
  parent: string,
  name: string,
  signal?: AbortSignal,
) =>
  apiPost(
    endpoint(device, '/directories'),
    { parent, name },
    (value) => {
      const path = safeString(safeObject(value).path)
      if (!path.startsWith('/') || path === '/')
        throw new ApiError('新建目录响应无效', 200, 'invalid_response')
      return path
    },
    signal,
  )
