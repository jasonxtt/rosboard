import { useCallback, useEffect, useRef, useState } from 'react'
import type { PolicyAuditPage, PolicyDiscovery, PolicyOverview, PolicyRulesPage } from './types'
import {
  fetchAudit,
  fetchDiscovery,
  fetchOverview,
  fetchSourceRules,
} from './api'

// ---- Overview polling hook ----

export function usePolicyOverview(deviceID: string, refreshNonce: number) {
  const [overview, setOverview] = useState<PolicyOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const reloadRef = useRef<(() => Promise<void>) | null>(null)
  const activeJobsRef = useRef(false)

  useEffect(() => {
    activeJobsRef.current = Boolean(overview?.activeJobs?.length)
  }, [overview])

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined
    const controller = new AbortController()

    const load = async () => {
      if (cancelled) return
      try {
        const data = await fetchOverview(deviceID, controller.signal)
        if (!cancelled) {
          setOverview(data)
          setError(null)
          setLoading(false)
        }
      } catch (e) {
        if (!cancelled && e instanceof DOMException && e.name === 'AbortError') return
        if (!cancelled) {
          setError(e instanceof Error ? e.message : '读取失败')
          setLoading(false)
        }
      }
    }

    reloadRef.current = async () => { await load() }

    const schedulePoll = () => {
      if (cancelled) return
      timer = window.setTimeout(async () => {
        if (document.visibilityState === 'visible') await load()
        schedulePoll()
      }, activeJobsRef.current ? 1000 : 15000)
    }

    void load().then(schedulePoll)

    return () => {
      cancelled = true
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [deviceID, refreshNonce])

  const reload = useCallback(async () => {
    if (reloadRef.current) await reloadRef.current()
  }, [])

  return { overview, loading, error, reload }
}

// ---- Discovery hook ----

export function usePolicyDiscovery(deviceID: string, enabled: boolean) {
  const [discovery, setDiscovery] = useState<PolicyDiscovery | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!enabled) {
      setDiscovery(null)
      setError(null)
      setLoading(false)
      return
    }
    let cancelled = false
    const controller = new AbortController()
    setDiscovery(null)
    setError(null)

    const load = async () => {
      setLoading(true)
      try {
        const data = await fetchDiscovery(deviceID, controller.signal)
        if (!cancelled) {
          setDiscovery(data)
          setError(null)
        }
      } catch (e) {
        if (!cancelled && !(e instanceof DOMException && e.name === 'AbortError')) {
          setDiscovery(null)
          setError(e instanceof Error ? e.message : '设备发现失败')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => { cancelled = true; controller.abort() }
  }, [deviceID, enabled])

  const reload = useCallback(async () => {
    try {
      const data = await fetchDiscovery(deviceID)
      setDiscovery(data)
      setError(null)
    } catch (e) {
      setDiscovery(null)
      setError(e instanceof Error ? e.message : '设备发现失败')
    }
  }, [deviceID])

  return { discovery, loading, error, reload }
}

// ---- Cursor pagination hook ----

export function useCursorPagination<TPage>(
  fetcher: (opts: { cursor?: string }) => Promise<TPage>,
  cacheKey: string,
  enabled: boolean,
  extract: (page: TPage) => { items: unknown[]; nextCursor: string },
) {
  const [pages, setPages] = useState<TPage[]>([])
  const [pageIndex, setPageIndex] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  useEffect(() => {
    setPages([])
    setPageIndex(0)
    setError(null)
    if (!enabled) return
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const page = await fetcherRef.current({})
        if (!cancelled) {
          setPages([page])
          setPageIndex(0)
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : '读取失败')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [cacheKey, enabled])

  const nextPage = useCallback(async () => {
    const current = pages[pageIndex]
    if (!current) return
    const { nextCursor } = extract(current)
    if (!nextCursor) return
    setLoading(true)
    setError(null)
    try {
      const page = await fetcherRef.current({ cursor: nextCursor })
      setPages((prev) => [...prev, page])
      setPageIndex((prev) => prev + 1)
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取失败')
    } finally {
      setLoading(false)
    }
  }, [pages, pageIndex, extract])

  const prevPage = useCallback(() => {
    setPageIndex((prev) => Math.max(0, prev - 1))
  }, [])

  const current = pages[pageIndex]
  const items = current ? extract(current).items : []

  return { items, pageIndex, pageCount: pages.length, loading, error, nextPage, prevPage }
}

// ---- Source rules pagination hook ----

export function useSourceRules(deviceID: string, sourceId: string, enabled: boolean) {
  return useCursorPagination(
    (opts) => fetchSourceRules(deviceID, sourceId, { limit: 200, ...opts }),
    `${deviceID}:${sourceId}`,
    enabled,
    (page: PolicyRulesPage) => ({ items: page.rules, nextCursor: page.nextCursor }),
  )
}

// ---- Audit pagination hook ----

export function usePolicyAudit(deviceID: string, enabled: boolean) {
  return useCursorPagination(
    (opts) => fetchAudit(deviceID, { limit: 100, ...opts }),
    deviceID,
    enabled,
    (page: PolicyAuditPage) => ({ items: page.entries, nextCursor: page.nextCursor }),
  )
}
