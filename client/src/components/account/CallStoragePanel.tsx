import { useCallback, useEffect, useState } from 'react'
import { api, type ArchiveCallsResult, type ArchiveJobStatus, type CallStorageStats } from '../../lib/api'
import { fmtBytes, fmtDateTime, getErrorMessage } from '../../lib/format'

function configStatus(enabled: boolean): string {
  return enabled ? 'ON' : 'OFF'
}

function configTone(enabled: boolean): string {
  return enabled ? 'text-console-accent' : 'text-console-muted'
}

type ArchiveProgress = {
  phase: 'starting' | 'batch' | 'done' | 'paused'
  batch: number
  initialRemaining: number
  remaining: number
  archived: number
  deleted: number
  freedBytes: number
  s3Dirs: number
  startedAt: number
  lastBatchSize: number
  statusLine: string
}

function fmtElapsed(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

function jobToProgress(job: ArchiveJobStatus): ArchiveProgress {
  const initialRemaining =
    job.initialRemaining && job.initialRemaining > 0
      ? job.initialRemaining
      : job.archived + job.remainingOld
  return {
    phase: job.running ? 'batch' : job.phase === 'paused' ? 'paused' : 'done',
    batch: job.batches ?? 0,
    initialRemaining,
    remaining: job.remainingOld,
    archived: job.archived,
    deleted: job.deleted,
    freedBytes: job.freedBytes,
    s3Dirs: job.s3DirsSynced,
    startedAt: (job.startedAt ?? Math.floor(Date.now() / 1000)) * 1000,
    lastBatchSize: job.lastBatchSize ?? 0,
    statusLine: job.statusLine || (job.running ? 'Archive running in background…' : 'Archive finished.'),
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    globalThis.window.setTimeout(resolve, ms)
  })
}

function isArchiveJobStatus(value: ArchiveCallsResult | ArchiveJobStatus): value is ArchiveJobStatus {
  return 'running' in value
}

function archiveProgressPct(progress: ArchiveProgress): number {
  if (progress.initialRemaining <= 0) return progress.phase === 'done' ? 100 : 0
  const done = progress.initialRemaining - progress.remaining
  return Math.min(100, Math.max(0, (done / progress.initialRemaining) * 100))
}

function ArchiveProgressPanel({ progress }: { progress: ArchiveProgress }) {
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const id = globalThis.window.setInterval(() => setNow(Date.now()), 1000)
    return () => globalThis.window.clearInterval(id)
  }, [])

  const pct = archiveProgressPct(progress)
  const elapsed = fmtElapsed(now - progress.startedAt)

  return (
    <div className="rounded border border-console-accent/40 bg-console-accent/5 p-3 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <p className="console-label text-[10px]">ARCHIVE IN PROGRESS</p>
        <span className="text-[10px] text-console-accent tracking-wider">
          {progress.phase === 'batch' ? '● PROCESSING' : progress.phase === 'starting' ? '● STARTING' : '● FINISHING'}
        </span>
      </div>

      <div className="h-2 rounded bg-console-border overflow-hidden">
        <div
          className="h-full bg-console-accent transition-[width] duration-500 ease-out"
          style={{ width: `${pct}%` }}
        />
      </div>

      <div className="flex items-center justify-between gap-2 text-[10px] text-console-muted tabular-nums">
        <span>{pct.toFixed(0)}% of backlog</span>
        <span>{elapsed} elapsed</span>
      </div>

      <div className="grid gap-2 sm:grid-cols-2 text-xs">
        <div className="text-console-muted">
          Batch <span className="text-console-text tabular-nums">{progress.batch}</span>
          {progress.lastBatchSize > 0 && (
            <span className="text-console-muted"> · last {progress.lastBatchSize.toLocaleString()}</span>
          )}
        </div>
        <div className="text-console-muted">
          Archived <span className="text-console-accent tabular-nums">{progress.archived.toLocaleString()}</span>
        </div>
        <div className="text-console-muted">
          Remaining <span className="text-console-text tabular-nums">{progress.remaining.toLocaleString()}</span>
        </div>
        <div className="text-console-muted">
          Freed <span className="text-console-text tabular-nums">{fmtBytes(progress.freedBytes)}</span>
        </div>
        {progress.s3Dirs > 0 && (
          <div className="text-console-muted sm:col-span-2">
            S3 day folders synced <span className="text-console-text tabular-nums">{progress.s3Dirs}</span>
          </div>
        )}
      </div>

      <p className="text-[11px] text-console-accent">{progress.statusLine}</p>
      <p className="text-[10px] text-console-muted">Keep this tab open for live progress, or return later — the job keeps running on the server.</p>
    </div>
  )
}

export function CallStoragePanel() {
  const [stats, setStats] = useState<CallStorageStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [lastResult, setLastResult] = useState<ArchiveCallsResult | null>(null)
  const [archiveProgress, setArchiveProgress] = useState<ArchiveProgress | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      setStats(await api.callStorageStats())
    } catch (err) {
      setError(getErrorMessage(err, 'Could not load call storage stats'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const pollArchiveJob = useCallback(async () => {
    for (;;) {
      const job = await api.archiveJobStatus()
      setArchiveProgress(jobToProgress(job))
      if (!job.running) {
        setLastResult(job)
        if (job.error) {
          setError(job.error)
        } else if (job.stoppedEarly) {
          setMessage(
            `Paused: ${job.archived.toLocaleString()} archived, ${job.remainingOld.toLocaleString()} still remaining. Run again to continue.`,
          )
        } else if (job.archived === 0) {
          setMessage('Nothing to archive.')
        } else {
          setMessage(
            `Done: ${job.archived.toLocaleString()} calls archived, ${fmtBytes(job.freedBytes)} freed${job.s3DirsSynced ? `, ${job.s3DirsSynced} day folder(s) synced to S3` : ''}${job.vacuumQueued ? '; database vacuum queued' : ''}.`,
          )
        }
        setArchiveProgress(null)
        await refresh()
        return
      }
      await sleep(2000)
    }
  }, [refresh])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const job = await api.archiveJobStatus()
        if (!job.running || cancelled) return
        setWorking(true)
        setArchiveProgress(jobToProgress(job))
        await pollArchiveJob()
      } catch {
        // ignore status probe on load
      } finally {
        if (!cancelled) setWorking(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [pollArchiveJob])

  async function runArchive(dryRun: boolean) {
    if (!stats?.retentionDays) {
      setError('Set CALL_RETENTION_DAYS in the stack env file before archiving.')
      return
    }
    if (!dryRun) {
      const ok = globalThis.window.confirm(
        `Archive and delete all calls older than ${stats.retentionDays} days? This runs in the background on the server until the backlog is cleared${stats.archiveS3Uri ? ' (uploading to Spaces)' : ''}.`,
      )
      if (!ok) return
    }
    setWorking(true)
    setError('')
    setMessage('')
    setArchiveProgress(null)
    try {
      if (dryRun) {
        const result = await api.archiveCalls({
          olderThanDays: stats.retentionDays,
          dryRun: true,
          async: false,
        })
        if (isArchiveJobStatus(result)) {
          throw new Error('unexpected background archive response for dry run')
        }
        setLastResult(result)
        setMessage(
          `Dry run: ${result.remainingOld.toLocaleString()} calls (${fmtBytes(result.freedBytes)}) would be archived.`,
        )
        return
      }

      const started = await api.archiveCalls({
        olderThanDays: stats.retentionDays,
        async: true,
        limit: 100,
      })
      if (!isArchiveJobStatus(started)) {
        throw new Error('expected background archive job')
      }
      setArchiveProgress(jobToProgress(started))
      await pollArchiveJob()
    } catch (err) {
      setError(getErrorMessage(err, 'Archive failed'))
      setArchiveProgress(null)
    } finally {
      setWorking(false)
    }
  }

  const archiveConfigured = Boolean(stats?.archiveDir?.trim())
  const envNotReachingContainer = stats && !stats.archiveDirFromEnv && !stats.retentionDaysFromEnv
  const s3Configured = Boolean(stats?.archiveS3Uri?.trim())

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <p className="console-label text-xs">CALL STORAGE & RETENTION</p>
        <button
          type="button"
          onClick={() => void refresh()}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
          disabled={loading || working}
        >
          {loading ? 'LOADING...' : 'REFRESH'}
        </button>
      </div>

      <p className="text-[11px] text-console-muted">
        Call audio lives in Postgres and can fill the database volume. Configure retention in the stack env file;
        <strong> Archive Now</strong> runs in the background on the server until the backlog is cleared (100 calls per batch).
      </p>

      {stats && (
        <div className="grid gap-3 md:grid-cols-2 text-xs">
          <div className="border border-console-border/70 rounded p-3 flex flex-col gap-2">
            <p className="console-label text-[10px]">DATABASE</p>
            <div className="text-console-muted">
              Calls: <span className="text-console-text tabular-nums">{stats.callCount.toLocaleString()}</span>
            </div>
            <div className="text-console-muted">
              Audio: <span className="text-console-text tabular-nums">{fmtBytes(stats.audioBytes)}</span>
            </div>
            <div className="text-console-muted">
              Oldest: <span className="text-console-text">{stats.oldestCallAt ? fmtDateTime(stats.oldestCallAt) : '—'}</span>
            </div>
            <div className="text-console-muted">
              Newest: <span className="text-console-text">{stats.newestCallAt ? fmtDateTime(stats.newestCallAt) : '—'}</span>
            </div>
          </div>

          <div className="border border-console-border/70 rounded p-3 flex flex-col gap-2">
            <p className="console-label text-[10px]">STACK CONFIG</p>
            <div className="text-console-muted">
              Retention:{' '}
              <span className={`tabular-nums ${stats.retentionDays > 0 ? 'text-console-accent' : 'text-console-error'}`}>
                {stats.retentionDays > 0 ? `${stats.retentionDays} days` : 'not set'}
              </span>
              {stats.retentionDaysFromEnv === false && stats.retentionDays > 0 && (
                <span className="text-console-muted"> (server default)</span>
              )}
            </div>
            <div className="text-console-muted break-all">
              Archive dir: <span className="text-console-text">{stats.archiveDir || '—'}</span>
              {stats.archiveDirFromEnv === false && stats.archiveDir && (
                <span className="text-console-muted"> (default — env not in container)</span>
              )}
            </div>
            <div className="text-console-muted break-all">
              S3 URI: <span className="text-console-text">{stats.archiveS3Uri || '—'}</span>
            </div>
            <div className="text-console-muted">
              Auto archive:{' '}
              <span className={configTone(stats.archiveLoopEnabled)}>{configStatus(stats.archiveLoopEnabled)}</span>
            </div>
            <div className="text-console-muted">
              Delete local after S3:{' '}
              <span className={configTone(stats.archiveDeleteLocalAfterS3)}>
                {configStatus(stats.archiveDeleteLocalAfterS3)}
              </span>
            </div>
          </div>
        </div>
      )}

      {envNotReachingContainer && (
        <div className="rounded border border-console-error/40 bg-console-error/10 px-3 py-2 text-[11px] text-console-error">
          Stack env vars are not reaching the api container. In Portainer → Stack → Environment, set{' '}
          <code>CALL_RETENTION_DAYS=30</code> and <code>CALL_ARCHIVE_DIR=/data/call-archive</code>, update the stack
          compose from the latest repo (must include pass-through lines under <code>api.environment</code>), then{' '}
          <strong>Update the stack</strong> with pull + recreate. Verify with{' '}
          <code>/api/v1/version</code> — look for <code>callRetentionDays</code> and <code>callArchiveDir</code>.
        </div>
      )}

      {archiveConfigured && stats && stats.retentionDays <= 0 && (
        <div className="rounded border border-console-error/40 bg-console-error/10 px-3 py-2 text-[11px] text-console-error">
          CALL_RETENTION_DAYS is not set. Add it to the Portainer stack Environment (e.g. <code>30</code>) and redeploy.
        </div>
      )}

      {archiveConfigured && stats && stats.retentionDays > 0 && !s3Configured && (
        <div className="rounded border border-console-border px-3 py-2 text-[11px] text-console-muted">
          S3 offload is optional. Set <code>CALL_ARCHIVE_S3_URI</code> and Spaces credentials in the stack env to upload archives to DigitalOcean Spaces.
        </div>
      )}

      <div className="flex gap-2 flex-wrap">
        <button
          type="button"
          onClick={() => void runArchive(true)}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
          disabled={working || !stats?.retentionDays}
        >
          {working ? 'WORKING...' : 'DRY RUN'}
        </button>
        <button
          type="button"
          onClick={() => void runArchive(false)}
          className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
          disabled={working || !stats?.retentionDays || !archiveConfigured}
        >
          {working ? 'ARCHIVING…' : 'ARCHIVE NOW'}
        </button>
      </div>

      {archiveProgress && working && <ArchiveProgressPanel progress={archiveProgress} />}

      {message && !working && <div className="text-[11px] text-console-accent">{message}</div>}
      {error && <div className="text-[11px] text-console-error">{error}</div>}
      {lastResult?.note && <div className="text-[10px] text-console-muted">{lastResult.note}</div>}

      <div className="text-[10px] text-console-muted border-t border-console-border/70 pt-2">
        Env example: <code>CALL_RETENTION_DAYS=30</code>, <code>CALL_ARCHIVE_DIR=/data/call-archive</code>,{' '}
        <code>CALL_ARCHIVE_S3_URI=s3://your-space/prefix</code>, <code>SPACES_ACCESS_KEY</code>,{' '}
        <code>SPACES_SECRET_KEY</code>, <code>SPACES_ENDPOINT=nyc3.digitaloceanspaces.com</code>
      </div>
    </div>
  )
}
