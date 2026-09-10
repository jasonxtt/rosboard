import { useCallback, useEffect, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { fetchDiagnostics } from './api'
import type { DiagnosticReport } from './types'

export type DiagnosticsState = {
  report: DiagnosticReport | null
  loading: boolean
  error: string | null
  reload: () => Promise<void>
}

export function useDiagnostics(deviceId: string): DiagnosticsState {
  const [report, setReport] = useState<DiagnosticReport | null>(null)
  const [loading, setLoading] = useState(Boolean(deviceId))
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(async () => {
    if (!deviceId) {
      setReport(null)
      setError(null)
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      const next = await fetchDiagnostics(deviceId)
      setReport(next)
      setError(null)
    } catch (loadError) {
      setError(errorMessage(loadError, '系统诊断读取失败'))
    } finally {
      setLoading(false)
    }
  }, [deviceId])

  useEffect(() => {
    if (!deviceId) {
      setReport(null)
      setError(null)
      setLoading(false)
      return
    }
    const controller = new AbortController()
    setLoading(true)
    fetchDiagnostics(deviceId, controller.signal)
      .then((next) => {
        setReport(next)
        setError(null)
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(loadError, '系统诊断读取失败'))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [deviceId])

  return { report, loading, error, reload }
}
