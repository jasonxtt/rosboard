import { useState } from 'react'
import { fetchTargetLists, type TargetList } from './canonical'
import { PolicyModal } from '../policy-routing/components'
import { TargetLibraryPage } from './TargetLibraryPage'

/** 在编辑器内管理已有目标库；关闭时回传最新列表供编辑器刷新。 */
export function TargetLibraryManagerModal({ deviceID, kind, onClose }: { deviceID: string; kind: 'domain' | 'ip'; onClose: (lists: TargetList[] | null) => void }) {
  const [closing, setClosing] = useState(false)
  // Hide immediately; the editor receives the refreshed lists when they arrive.
  const close = async () => {
    setClosing(true)
    try { onClose(await fetchTargetLists(deviceID)) } catch { onClose(null) }
  }
  if (closing) return null
  return <PolicyModal title={kind === 'ip' ? '管理 IP 目标库' : '管理域名目标库'} wide onClose={() => void close()}>
    <TargetLibraryPage deviceID={deviceID} refreshNonce={0} kind={kind} />
  </PolicyModal>
}
