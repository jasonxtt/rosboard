import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { Button } from '../../../ui/Button'
import { EmptyState } from '../../../ui/EmptyState'
import { Modal } from '../../../ui/Modal'
import { SearchInput } from '../../../ui/SearchInput'
import { Skeleton } from '../../../ui/Skeleton'
import { fetchTargetListRules } from '../api'
import type { TargetList, TargetListRule } from '../canonical'
import { formatCount } from '../../../lib/format'
import { errorMessage } from '../../../lib/api'
import { Notice } from './Notice'
import { kindLabel } from './labels'

type RulesViewerModalProps = {
  deviceID: string
  target: TargetList
  onClose: () => void
}

const PAGE_SIZE = 100
const SEARCH_DEBOUNCE_MS = 300

/** 规则查看器: target list contents with search + cursor load-more. */
export function RulesViewerModal({ deviceID, target, onClose }: RulesViewerModalProps) {
  const [query, setQuery] = useState('')
  const [rules, setRules] = useState<TargetListRule[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [versionId, setVersionId] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const requestSeq = useRef(0)

  const load = useCallback(
    async (search: string, cursor: string, append: boolean) => {
      const seq = ++requestSeq.current
      if (append) setLoadingMore(true)
      else setLoading(true)
      setError(null)
      try {
        const page = await fetchTargetListRules(deviceID, target.id, { limit: PAGE_SIZE, cursor: cursor || undefined, query: search || undefined })
        if (requestSeq.current !== seq) return
        setRules((current) => (append ? [...current, ...page.rules] : page.rules))
        setNextCursor(page.nextCursor)
        setVersionId(page.versionId)
      } catch (loadError) {
        if (requestSeq.current !== seq) return
        setError(errorMessage(loadError, '规则读取失败'))
      } finally {
        if (requestSeq.current === seq) {
          setLoading(false)
          setLoadingMore(false)
        }
      }
    },
    [deviceID, target.id],
  )

  useEffect(() => {
    const timer = window.setTimeout(() => void load(query.trim(), '', false), SEARCH_DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [load, query])

  return (
    <Modal open onClose={onClose} title={`规则内容：${target.name}`} maxWidth={680}>
      <div className="pol-rules-toolbar">
        <SearchInput value={query} onChange={setQuery} placeholder={`搜索${kindLabel(target.kind)}规则`} ariaLabel="搜索规则" width={260} />
        <Badge tone="neutral">{kindLabel(target.kind)}</Badge>
        {versionId ? <Badge tone="neutral">版本 {versionId.slice(0, 8)}</Badge> : null}
        <Badge tone="accent">{formatCount(rules.length)} 条已载入</Badge>
      </div>
      {error ? <Notice tone="err">{error}</Notice> : null}
      {loading ? (
        <Skeleton lines={8} height={13} />
      ) : rules.length ? (
        <div className="pol-rules-table table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th style={{ width: '110px' }}>类型</th>
                <th>{target.kind === 'ip' ? '地址' : '域名'}</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((rule, index) => (
                <tr key={`${rule.type}:${rule.domain ?? rule.address ?? ''}:${index}`}>
                  <td>{rule.type}</td>
                  <td className="pol-rule-value">{rule.domain ?? rule.address ?? ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState icon="🎯" title={query ? '没有匹配的规则' : '此目标库暂无规则内容'} description={query ? '换个关键词试试。' : '目标库内容会在创建或刷新后填充。'} />
      )}
      {nextCursor && !loading ? (
        <div className="pol-rules-more">
          <Button loading={loadingMore} onClick={() => void load(query.trim(), nextCursor, true)}>
            加载更多
          </Button>
        </div>
      ) : null}
    </Modal>
  )
}
