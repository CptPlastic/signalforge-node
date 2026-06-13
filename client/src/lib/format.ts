import { ApiError, type VersionInfo } from './api'

export function fmtTime(unix: number): string {
  const d = new Date(unix * 1000)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
  }
  return (
    d.toLocaleDateString([], { month: 'short', day: 'numeric' }) +
    ' ' +
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
  )
}

export function fmtFreq(hz: number): string {
  return hz > 0 ? `${(hz / 1_000_000).toFixed(4)} MHz` : '—'
}

export function fmtDur(s: number): string {
  return s > 0 ? `${s.toFixed(1)}s` : '—'
}

export function fmtBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = value >= 100 || unit === 0 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(digits)} ${units[unit]}`
}

export function shortCommit(commit: string): string {
  if (!commit || commit === 'unknown') return 'unknown'
  return commit.slice(0, 8)
}

export function fmtBuildDate(buildDate: string): string {
  if (!buildDate || buildDate === 'unknown') return 'unknown date'
  const parsed = new Date(buildDate)
  if (Number.isNaN(parsed.getTime())) return buildDate
  return parsed.toISOString().replace('T', ' ').slice(0, 19) + 'Z'
}

function known(value: string | undefined): boolean {
  return Boolean(value?.trim() && value !== 'unknown')
}

export function deploymentHeaderLabel(info: VersionInfo | null): string {
  if (!info) return 'loading'
  const commit = known(info.commit) ? info.commit.slice(0, 8) : ''
  const stack = known(info.stackId) ? info.stackId : ''
  const tag = known(info.deployTag) ? info.deployTag : ''

  if (commit && stack && tag) return `${commit} • ${stack}:${tag}`
  if (commit) return commit
  if (stack && tag) return `${stack}:${tag}`
  if (stack) return stack
  if (known(info.version)) return info.version
  return 'local build'
}

export function deploymentTitle(info: VersionInfo | null): string {
  if (!info) return 'deployment identity loading'
  const lines = [
    known(info.version) ? `version: ${info.version}` : '',
    known(info.commit) ? `commit: ${info.commit}` : '',
    known(info.buildDate) ? `build: ${fmtBuildDate(info.buildDate)}` : '',
    known(info.environment) ? `environment: ${info.environment}` : '',
    known(info.stackId) ? `stack: ${info.stackId}` : '',
    known(info.deployTag) ? `deploy tag: ${info.deployTag}` : '',
  ].filter(Boolean)
  return lines.length > 0 ? lines.join('\n') : 'local development build'
}

export function deploymentFooterLabel(info: VersionInfo | null): string {
  if (!info) return 'deployment identity loading'
  const parts = [
    known(info.stackId) ? `stack=${info.stackId}` : '',
    known(info.deployTag) ? `tag=${info.deployTag}` : '',
    known(info.commit) ? `commit=${info.commit.slice(0, 8)}` : '',
    known(info.buildDate) ? `build=${fmtBuildDate(info.buildDate)}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' ') : 'local development build'
}

export function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    if (typeof err.body === 'string' && err.body.trim()) {
      return err.body
    }
    return `${fallback} (${err.status})`
  }
  if (err instanceof Error && err.message) {
    return `${fallback}: ${err.message}`
  }
  return fallback
}

export function fmtDateTime(unix: number): string {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString()
}
