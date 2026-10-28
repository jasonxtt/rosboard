import TargetLibraryPage from '../../../pages/TargetLibraryPage'
import { Modal } from '../../../ui/Modal'
import { fetchTargetLists, type TargetList } from '../canonical'

/** 在编辑器内管理已有目标库；关闭时回传最新列表供编辑器刷新。 */
export function TargetLibraryManagerModal({ deviceID, kind, onClose }: { deviceID: string; kind: 'domain' | 'ip'; onClose: (lists: TargetList[] | null) => void }) {
  const close = async () => {
    try {
      onClose(await fetchTargetLists(deviceID))
    } catch {
      onClose(null)
    }
  }
  return (
    <Modal open title={kind === 'ip' ? '管理 IP 目标库' : '管理域名目标库'} maxWidth={960} onClose={() => void close()}>
      <TargetLibraryPage kind={kind} />
    </Modal>
  )
}
