import { useCallback, useEffect, useState } from 'react'
import { errorMessage } from '../../lib/api'
import { fetchDeepDiagnostics, fetchDiagnostics } from './api'
import type { DeepDiagnosticReport, DiagnosticReport } from './types'

export type DiagnosticsState = {
  report: DiagnosticReport | null
  loading: boolean
  error: string | null
  reload: () => Promise<void>
  deepReport: DeepDiagnosticReport | null
  deepLoading: boolean
  deepError: string | null
  runDeep: () => Promise<void>
}

export function useDiagnostics(deviceId: string): DiagnosticsState {
  const [report, setReport] = useState<DiagnosticReport | null>(null)
  const [loading, setLoading] = useState(Boolean(deviceId))
  const [error, setError] = useState<string | null>(null)
  const [deepReport, setDeepReport] = useState<DeepDiagnosticReport | null>(null)
  const [deepLoading, setDeepLoading] = useState(false)
  const [deepError, setDeepError] = useState<string | null>(null)

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

  const runDeep = useCallback(async () => {
    if (!deviceId) {
      setDeepReport(null)
      setDeepError(null)
      setDeepLoading(false)
      return
    }
    setDeepLoading(true)
    try {
      const next = await fetchDeepDiagnostics(deviceId)
      setDeepReport(next)
      setDeepError(null)
    } catch (loadError) {
      setDeepError(errorMessage(loadError, '全面体检读取失败'))
    } finally {
      setDeepLoading(false)
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

  useEffect(() => {
    setDeepReport(null)
    setDeepError(null)
    setDeepLoading(false)
  }, [deviceId])

  return { report, loading, error, reload, deepReport, deepLoading, deepError, runDeep }
}
