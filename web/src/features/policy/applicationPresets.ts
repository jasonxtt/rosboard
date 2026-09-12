import { useCallback, useEffect, useState } from 'react'
import { applicationPresetCatalogErrorMessage, fetchApplicationPresets, type ApplicationPreset } from './canonical'

export function useApplicationPresets() {
  const [presets, setPresets] = useState<ApplicationPreset[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadNonce, setReloadNonce] = useState(0)

  const reload = useCallback(() => setReloadNonce((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    void fetchApplicationPresets(controller.signal)
      .then((items) => {
        if (!cancelled) setPresets(items)
      })
      .catch((fetchError) => {
        if (!cancelled && !controller.signal.aborted) setError(applicationPresetCatalogErrorMessage(fetchError))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [reloadNonce])

  return { presets, loading, error, reload }
}
