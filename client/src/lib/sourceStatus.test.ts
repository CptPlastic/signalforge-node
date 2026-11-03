import { describe, expect, it } from 'vitest'
import type { IngestionSource } from './api'
import { getSourceRuntimeStatus, SOURCE_STALE_AFTER_SEC } from './sourceStatus'

const source = (overrides: Partial<IngestionSource> = {}): IngestionSource => ({
  id: 'source-1',
  label: 'Source 1',
  enabled: true,
  isShared: false,
  systemId: 0,
  systemLabel: '',
  lastSeenUnix: 0,
  errorCount: 0,
  callsReceived: 0,
  updatedAt: 0,
  ...overrides,
})

describe('getSourceRuntimeStatus', () => {
  it('waits neutrally for a source that has not received a call', () => {
    expect(getSourceRuntimeStatus(source(), 1_000).state).toBe('waiting')
  })

  it('keeps normally quiet sources live for ten minutes after receipt', () => {
    const now = 10_000
    expect(getSourceRuntimeStatus(source({ lastSeenUnix: now - SOURCE_STALE_AFTER_SEC }), now).state).toBe('live')
    expect(getSourceRuntimeStatus(source({ lastSeenUnix: now - SOURCE_STALE_AFTER_SEC - 1 }), now).state).toBe('offline')
  })

  it('reports errors only while the source was recently heard from', () => {
    const now = 10_000
    expect(getSourceRuntimeStatus(source({ lastSeenUnix: now - 60, errorCount: 1 }), now).state).toBe('error')
    expect(getSourceRuntimeStatus(source({ lastSeenUnix: now - SOURCE_STALE_AFTER_SEC - 1, errorCount: 1 }), now).state).toBe('offline')
  })
})
