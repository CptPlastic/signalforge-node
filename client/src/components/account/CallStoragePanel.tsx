import { useCallback, useEffect, useState } from 'react'
import { api, type ArchiveCallsResult, type CallStorageStats } from '../../lib/api'
import { fmtBytes, fmtDateTime, getErrorMessage } from '../../lib/format'

function configStatus(enabled: boolean): string {
  return enabled ? 'ON' : 'OFF'
}

function configTone(enabled: boolean): string {
  return enabled ? 'text-console-accent' : 'text-console-muted'
}

export function CallStoragePanel() {
  const [stats, setStats] = useState<CallStorageStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [lastResult, setLastResult] = useState<ArchiveCallsResult | null>(null)

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

  async function runArchive(dryRun: boolean) {
    if (!stats?.retentionDays) {
      setError('Set CALL_RETENTION_DAYS in the stack env file before archiving.')
      return
    }
    if (!dryRun) {
      const ok = globalThis.window.confirm(
        `Archive and delete calls older than ${stats.retentionDays} days? Audio is exported first${stats.archiveS3Uri ? ' to Spaces' : ''}.`,
      )
      if (!ok) return
    }
    setWorking(true)
    setError('')
    setMessage('')
    try {
      const result = await api.archiveCalls({
        olderThanDays: stats.retentionDays,
        dryRun,
        limit: 100,
      })
      setLastResult(result)
      if (dryRun) {
        setMessage(
          `Dry run: ${result.remainingOld.toLocaleString()} calls (${fmtBytes(result.freedBytes)}) would be archived.`,
        )
      } else {
        setMessage(
          `Archived ${result.archived} calls, deleted ${result.deleted}, freed ${fmtBytes(result.freedBytes)}${result.s3DirsSynced ? `, synced ${result.s3DirsSynced} day folder(s) to S3` : ''}${result.vacuumQueued ? '; database vacuum queued' : ''}.`,
        )
        await refresh()
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Archive failed'))
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
        this panel shows live status and lets you run archive batches.
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
          {working ? 'WORKING...' : 'ARCHIVE NOW'}
        </button>
      </div>

      {message && <div className="text-[11px] text-console-accent">{message}</div>}
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
