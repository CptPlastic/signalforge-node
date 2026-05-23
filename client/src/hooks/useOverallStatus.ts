import { useMemo } from 'react'
import type { AuthUser, IngestionSource } from '../lib/api'
import { getSourceRuntimeStatus, type SmoothedSourceStatus } from '../lib/sourceStatus'
import type { OverallStatus } from '../types/app'

const WS_RECONNECT_GRACE_SEC = 8
const API_HEALTH_GRACE_SEC = 8

type UseOverallStatusArgs = Readonly<{
  apiHealthy: boolean
  apiLastHealthyUnix: number | null
  authUser: AuthUser | null
  connected: boolean
  nowUnix: number
  sourceStatusMap: Record<string, SmoothedSourceStatus>
  sourcesMap: Record<string, IngestionSource>
  wsLastDisconnectUnix: number | null
}>

export function useOverallStatus({
  apiHealthy,
  apiLastHealthyUnix,
  authUser,
  connected,
  nowUnix,
  sourceStatusMap,
  sourcesMap,
  wsLastDisconnectUnix,
}: UseOverallStatusArgs): OverallStatus {
  return useMemo(() => {
    const sources = Object.values(sourcesMap)
    const enabledSources = sources.filter((source) => source.enabled)
    const sourceStates = new Set(enabledSources.map((source) => (sourceStatusMap[source.id] ?? getSourceRuntimeStatus(source, nowUnix)).state))

    const wsWithinGraceWindow = wsLastDisconnectUnix !== null && (nowUnix - wsLastDisconnectUnix) <= WS_RECONNECT_GRACE_SEC
    const apiWithinGraceWindow = apiLastHealthyUnix !== null && (nowUnix - apiLastHealthyUnix) <= API_HEALTH_GRACE_SEC

    if (!apiHealthy && apiWithinGraceWindow) {
      return {
        dotClass: 'bg-console-muted',
        label: 'RECONNECTING',
        title: 'api reconnecting',
      }
    }

    if (!apiHealthy) {
      return {
        dotClass: 'bg-console-error',
        label: 'OFFLINE',
        title: 'api unavailable',
      }
    }

    if (!authUser) {
      return {
        dotClass: 'bg-console-accent',
        label: 'OPERATIONAL',
        title: 'api healthy (sign in required for websocket/live monitor)',
      }
    }

    if (!connected && wsWithinGraceWindow) {
      return {
        dotClass: 'bg-console-muted',
        label: 'RECONNECTING',
        title: 'websocket reconnecting',
      }
    }

    if (!connected) {
      return {
        dotClass: 'bg-console-muted',
        label: 'RECONNECTING',
        title: 'websocket reconnecting',
      }
    }

    if (enabledSources.length === 0) {
      return {
        dotClass: 'bg-console-muted',
        label: 'IDLE',
        title: 'no enabled sources',
      }
    }

    if (sourceStates.has('error')) {
      return {
        dotClass: 'bg-console-error',
        label: 'DEGRADED',
        title: 'one or more enabled sources report errors',
      }
    }

    if (sourceStates.has('offline')) {
      return {
        dotClass: 'bg-console-amber',
        label: 'DEGRADED',
        title: 'one or more enabled sources are inactive',
      }
    }

    return {
      dotClass: 'bg-console-accent',
      label: 'OPERATIONAL',
      title: 'api, websocket, and enabled sources are healthy',
    }
  }, [apiHealthy, apiLastHealthyUnix, authUser, connected, nowUnix, sourceStatusMap, sourcesMap, wsLastDisconnectUnix])
}