import type { UpdateManifest } from './api'

export type RunningRelease = {
  deployTag?: string
  commit?: string
  buildDate?: string
}

const UNORDERED = new Set(['', 'unknown', 'latest'])

function normalize(value: string | undefined): string {
  return (value || '').trim().toLowerCase()
}

function sameRevision(left: string | undefined, right: string | undefined): boolean {
  const a = normalize(left)
  const b = normalize(right)
  if (UNORDERED.has(a) || UNORDERED.has(b)) return false
  if (a === b) return true
  const [shorter, longer] = a.length <= b.length ? [a, b] : [b, a]
  return shorter.length >= 7 && longer.startsWith(shorter)
}

export function sameRelease(current: RunningRelease, latest: UpdateManifest | undefined): boolean {
  if (!latest) return false
  const running = [current.commit, current.deployTag]
  const published = [latest.commit, latest.imageTag, latest.shortCommit, latest.version]
  return running.some((left) => published.some((right) => sameRevision(left, right)))
}

// Short image tags are not ordered. A published manifest is newer only when
// its publishedAt is after this image was built.
export function isNewerPublishedRelease(current: RunningRelease, latest: UpdateManifest | undefined): boolean {
  if (!latest || sameRelease(current, latest)) return false
  const published = Date.parse(latest.publishedAt || '')
  const built = Date.parse(current.buildDate || '')
  if (!Number.isFinite(published) || !Number.isFinite(built)) return false
  return published > built
}
