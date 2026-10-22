import { newDraft } from './drafts'
import type { Draft } from './types'

export type ComposeFields = Pick<Draft,
  'name' | 'image' | 'env' | 'mounts' | 'command' | 'entrypoint' | 'user' |
  'workdir' | 'memoryHigh' | 'memoryMax' | 'cpuList' | 'restartPolicy'>
export type ComposeNotice = { field: string; message: string }
export type ComposeService = {
  name: string
  fields: ComposeFields
  imported: string[]
  skipped: ComposeNotice[]
  needsInput: ComposeNotice[]
  mountHints: string[]
}
export type ComposeResult = { format?: 'compose' | 'docker-run'; services: ComposeService[]; skipped: ComposeNotice[] }

export function composeFields(d: Draft): ComposeFields {
  return {
    name: d.name, image: d.image, env: d.env, mounts: d.mounts,
    command: d.command, entrypoint: d.entrypoint, user: d.user,
    workdir: d.workdir, memoryHigh: d.memoryHigh, memoryMax: d.memoryMax,
    cpuList: d.cpuList, restartPolicy: d.restartPolicy,
  }
}

export function applyCompose(draft: Draft, fields: ComposeFields): Draft {
  const defaults = newDraft()
  return {
    ...draft,
    ...composeFields({ ...defaults, ...fields }),
    // Docker host paths never cross into RouterOS source paths, even if a
    // malformed/older response includes one. Existing mutation validation stays.
    mounts: fields.mounts.map((m) => ({ ...m, source: '' })),
    env: fields.env.map((e) => ({ ...e })),
    imageSource: 'registry', archiveId: '', archiveFile: '',
  }
}
