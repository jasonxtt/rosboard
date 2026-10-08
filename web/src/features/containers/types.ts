export type Network = {
  veth: string
  bridge: string
  address: string
  gateway: string
  address6: string
  gateway6: string
  mac: string
}
export type Environment = { key: string; value: string }
export type Mount = {
  source: string
  target: string
  readOnly: boolean
  mode?: string
}
export type Health = {
  mode: string
  command: string
  interval: string
  timeout: string
  retries: string
  startPeriod: string
}
export type ImageArchive = {
  id: string
  name: string
  reference: string
  architecture: string
  bytes: number
  sha256: string
  remotePath: string
}
export type DirectoryEntry = {
  id: string
  protected: string
  name: string
  path: string
  directory: boolean
  bytes: number
}
export type DirectoryRequest = {
  action: 'mkdir' | 'rename' | 'delete' | 'recover'
  requestId: string
  parent?: string
  path?: string
  name?: string
  expectedId?: string
  confirmPath?: string
}
export type DirectoryMutation = {
  action: DirectoryRequest['action']
  requestId: string
  path: string
  previousPath: string
  state: 'pending' | 'succeeded' | 'unknown'
}
export type DirectoryListing = {
  id: string
  canCreate: boolean
  pending: DirectoryMutation | null
  path: string
  entries: DirectoryEntry[]
}
export type Draft = {
  draftId: string
  existingId: string
  name: string
  image: string
  imageSource: string
  archiveId: string
  archiveFile: string
  network: Network
  rootDir: string
  command: string
  entrypoint: string
  user: string
  workdir: string
  env: Environment[]
  mounts: Mount[]
  memoryHigh: string
  memoryMax: string
  cpuList: string
  startAfterCreate: boolean
  startOnBoot: boolean
  logging: boolean
  restartPolicy: string
  health: Health
}
export type Item = {
  id: string
  name: string
  status: string
  image: string
  network: Network
  cpu: string
  memory: string
  startOnBoot: boolean
  ownership: string
  sharedVeth: string[]
  envLists: string[]
  mountLists: string[]
  config: Draft
  imageDefaults: Record<string, string>
}
export type Options = {
  architecture: string
  archives: ImageArchive[]
  bridges: string[]
  interfaces: string[]
  usedIPs: string[]
  disks: { name: string; freeBytes: number; writable: boolean }[]
  memoryHigh: string
  memoryMax: string
}
export type Capabilities = {
  directoryWrites: boolean
  supported: boolean
  writes: boolean
  mode: string
  version: string
  logs: boolean
  fields: string[]
  warnings: string[]
}
export type Snapshot = {
  activeJob: Job | null
  items: Item[]
  options: Options
  capabilities: Capabilities
}
export type Resolution = {
  effective: Draft
  errors: Record<string, string>
  defaults: string[]
  containerFields: Record<string, string>
}
export type Action =
  | 'create'
  | 'edit'
  | 'start'
  | 'stop'
  | 'restart'
  | 'update'
  | 'delete'
  | 'adopt'
export type Job = {
  id: string
  deviceId: string
  action: string
  targetId: string
  state: string
  phase: string
  progress: number
  error: string
  retained: string[]
}
export type ContainerLog = { id: string; time: string; message: string }
