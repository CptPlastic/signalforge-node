import type { IngestionSource } from './api'

export const SOURCE_STALE_AFTER_SEC = 12
export const SOURCE_STATUS_MIN_DWELL_SEC = 6
export const SOURCE_OFFLINE_GRACE_SEC = 10

export type SourceRuntimeStatus = {
  state: 'live' | 'offline' | 'disabled' | 'error'
  dotClass: string
  title: string
  label: string
}

export type SmoothedSourceStatus = SourceRuntimeStatus & {
  changedAtUnix: number
}

export function getSourceRuntimeStatus(source: IngestionSource, nowUnix: number): SourceRuntimeStatus {
  if (!source.enabled) {
    return {
      state: 'disabled',
      dotClass: 'bg-console-muted',
      title: 'disabled',
      label: 'DISABLED',
    }
  }

  if (source.lastSeenUnix <= 0) {
    if (source.errorCount > 0) {
      return {
        state: 'error',
        dotClass: 'bg-console-error',
        title: `${source.errorCount} errors`,
        label: 'ERROR',
      }
    }
    return {
      state: 'offline',
      dotClass: 'bg-console-amber',
      title: 'degraded (no calls received yet)',
      label: 'DEGRADED',
    }
  }

  const ageSec = nowUnix - source.lastSeenUnix
  if (ageSec > SOURCE_STALE_AFTER_SEC) {
    if (source.errorCount > 0) {
      return {
        state: 'error',
        dotClass: 'bg-console-error',
        title: `offline - ${source.errorCount} errors`,
        label: 'ERROR',
      }
    }
    return {
      state: 'offline',
      dotClass: 'bg-console-amber',
      title: `degraded (${ageSec}s since last call)`,
      label: 'DEGRADED',
    }
  }

  if (source.errorCount > 0) {
    return {
      state: 'live',
      dotClass: 'bg-console-accent',
      title: `live - ${source.errorCount} past errors`,
      label: 'LIVE',
    }
  }

  return {
    state: 'live',
    dotClass: 'bg-console-accent',
    title: 'live',
    label: 'LIVE',
  }
}

export function smoothSourceStatusMap(
  previous: Record<string, SmoothedSourceStatus>,
  sourcesMap: Record<string, IngestionSource>,
  nowUnix: number,
): Record<string, SmoothedSourceStatus> {
  const next: Record<string, SmoothedSourceStatus> = {}

  Object.values(sourcesMap).forEach((source) => {
    const rawStatus = getSourceRuntimeStatus(source, nowUnix)
    const existing = previous[source.id]

    if (!existing) {
      next[source.id] = { ...rawStatus, changedAtUnix: nowUnix }
      return
    }

    if (existing.state === rawStatus.state) {
      next[source.id] = existing
      return
    }

    const ageSec = nowUnix - source.lastSeenUnix
    const isLiveOfflineToggle =
      (existing.state === 'live' && rawStatus.state === 'offline') ||
      (existing.state === 'offline' && rawStatus.state === 'live')

    if (isLiveOfflineToggle) {
      const withinMinDwell = (nowUnix - existing.changedAtUnix) < SOURCE_STATUS_MIN_DWELL_SEC
      const withinOfflineGrace = rawStatus.state === 'offline' && ageSec <= (SOURCE_STALE_AFTER_SEC + SOURCE_OFFLINE_GRACE_SEC)
      if (withinMinDwell || withinOfflineGrace) {
        next[source.id] = existing
        return
      }
    }

    next[source.id] = { ...rawStatus, changedAtUnix: nowUnix }
  })

  return next
}