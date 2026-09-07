import { useEffect, useRef, useState } from 'react'
import { Badge } from '../../../ui/Badge'
import { fetchAccessJob, fetchPolicyJob, JOB_FAILED_STATES, type PolicyJob } from '../api'
import { jobPhaseLabel } from './labels'

type JobProgressProps = {
  deviceID: string
  domain: 'policy' | 'access'
  jobId: string
  /** e.g. 「正在应用分流规则」 */
  label?: string
  onCommitted?: (job: PolicyJob) => void
  onFailed?: (job: PolicyJob) => void
}

const POLL_MS = 700

/**
 * Live job progress bar. Polls the job endpoint until a terminal state,
 * then fires onCommitted/onFailed exactly once.
 */
export function JobProgress({ deviceID, domain, jobId, label, onCommitted, onFailed }: JobProgressProps) {
  const [job, setJob] = useState<PolicyJob | null>(null)
  const callbacksRef = useRef({ onCommitted, onFailed })
  callbacksRef.current = { onCommitted, onFailed }

  useEffect(() => {
    let cancelled = false
    let timer = 0
    const fetcher = domain === 'access' ? fetchAccessJob : fetchPolicyJob
    const tick = async () => {
      try {
        const snapshot = await fetcher(deviceID, jobId)
        if (cancelled) return
        setJob(snapshot)
        if (snapshot.state === 'committed') {
          callbacksRef.current.onCommitted?.(snapshot)
          return
        }
        if (JOB_FAILED_STATES.has(snapshot.state)) {
          callbacksRef.current.onFailed?.(snapshot)
          return
        }
        timer = window.setTimeout(() => void tick(), POLL_MS)
      } catch {
        if (cancelled) return
        // A missing/failed job read is terminal for the progress UI; surface it as failed.
        callbacksRef.current.onFailed?.({ id: jobId, state: 'failed', phase: '', progress: 0, error: '任务状态读取失败' })
      }
    }
    void tick()
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [deviceID, domain, jobId])

  const progress = job ? Math.min(100, Math.max(0, job.progress)) : 0
  return (
    <div className="pol-job" aria-live="polite">
      <div className="pol-job-head">
        <Badge tone="accent" dot>
          {job ? jobPhaseLabel(job) : '排队中'}
        </Badge>
        <span className="pol-job-label">{label ?? '正在应用到 RouterOS'}</span>
        <span className="pol-job-pct">{progress}%</span>
      </div>
      <div className="pol-progress" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}>
        <div className="pol-progress-bar" style={{ width: `${progress}%` }} />
      </div>
    </div>
  )
}

/** Static progress row for jobs discovered via overview polling. */
export function JobProgressLine({ job, label }: { job: PolicyJob; label?: string }) {
  const progress = Math.min(100, Math.max(0, job.progress))
  return (
    <div className="pol-job">
      <div className="pol-job-head">
        <Badge tone="accent" dot>
          {jobPhaseLabel(job)}
        </Badge>
        <span className="pol-job-label">{label ?? '正在应用到 RouterOS'}</span>
        <span className="pol-job-pct">{progress}%</span>
      </div>
      <div className="pol-progress" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}>
        <div className="pol-progress-bar" style={{ width: `${progress}%` }} />
      </div>
    </div>
  )
}
