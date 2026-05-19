import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiError,
  api,
  type AuthUser,
  type AuditLogEntry,
  type Call,
  type HubIdentity,
  type HubInvite,
  type HubPeer,
  type IngestionSource,
  type RadioSet,
  type SourceAPIKey,
  type TalkgroupInfo,
  type TalkgroupSetting,
  type UpdateCheckResponse,
  type UserRecord,
  type VersionInfo,
} from './lib/api'
import { CallRow } from './components/calls/CallRow'
import { deploymentFooterLabel, deploymentHeaderLabel, deploymentTitle, fmtDateTime, fmtTime, getErrorMessage } from './lib/format'
import { WebSocketClient } from './lib/ws'

type WsCallEvent = { type: 'call'; call: Call; sourceId?: string }
type WsSourceDeletedEvent = { type: 'source_deleted'; sourceId: string }
type WsHeartbeatEvent = { type: 'heartbeat'; ts: number }
type WsEvent = WsCallEvent | WsSourceDeletedEvent | WsHeartbeatEvent
type AppView = 'monitor' | 'radio-sets' | 'integrations' | 'talkgroups' | 'hub' | 'account'

type HubIdentityDraft = Pick<HubIdentity, 'name' | 'publicUrl' | 'region' | 'contact' | 'federationEnabled'>

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>
}

const SOURCE_STALE_AFTER_SEC = 12
const SOURCE_STATUS_MIN_DWELL_SEC = 6
const SOURCE_OFFLINE_GRACE_SEC = 10
const WS_RECONNECT_GRACE_SEC = 8
const API_HEALTH_GRACE_SEC = 8
const SESSION_WARNING_WINDOW_SEC = 15 * 60
const CALL_PAGE_SIZE = 50
const DEFAULT_SOURCE_REPO_URL = 'https://github.com/CptPlastic/signalforge-node'

const sourceRepoURL = (import.meta.env.VITE_SIGNALFORGE_SOURCE_REPO_URL?.trim() || DEFAULT_SOURCE_REPO_URL).replace(/\/+$/, '')
const configuredSourceURL = import.meta.env.VITE_SIGNALFORGE_SOURCE_URL?.trim()
const configuredLicenseURL = import.meta.env.VITE_SIGNALFORGE_LICENSE_URL?.trim()
const configuredFairSourceURL = import.meta.env.VITE_SIGNALFORGE_FAIR_SOURCE_URL?.trim()

type SourceRuntimeStatus = {
  state: 'live' | 'offline' | 'disabled' | 'error'
  dotClass: string
  title: string
  label: string
}

type SmoothedSourceStatus = SourceRuntimeStatus & {
  changedAtUnix: number
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(console.error)
}

function sourceReference(info: VersionInfo | null): string {
  const commit = info?.commit?.trim()
  return commit && commit !== 'unknown' ? commit : 'main'
}

function sourceLinks(info: VersionInfo | null) {
  const ref = sourceReference(info)
  return {
    source: configuredSourceURL || `${sourceRepoURL}/tree/${ref}`,
    license: configuredLicenseURL || `${sourceRepoURL}/blob/${ref}/LICENSE`,
    fairSource: configuredFairSourceURL || `${sourceRepoURL}/blob/${ref}/FAIR-SOURCE.md`,
  }
}

function redactKey(key: string): string {
  if (key.length <= 8) return key
  return '*'.repeat(key.length - 4) + key.slice(-4)
}

function upsertTalkgroupInfo(talkgroups: TalkgroupInfo[], call: Call): TalkgroupInfo[] {
  if (call.talkgroup <= 0) return talkgroups

  const nextInfo: TalkgroupInfo = {
    talkgroup: call.talkgroup,
    talkgroupLabel: call.talkgroupLabel,
    talkgroupGroup: call.talkgroupGroup,
  }

  if (!talkgroups.some((tg) => tg.talkgroup === call.talkgroup)) {
    return [...talkgroups, nextInfo].sort((a, b) => a.talkgroup - b.talkgroup)
  }

  return talkgroups.map((tg) => tg.talkgroup === call.talkgroup ? { ...tg, ...nextInfo } : tg)
}

function removeTalkgroupFromRadioSets(sets: RadioSet[], talkgroup: number): RadioSet[] {
  return sets.map((set) => ({
    ...set,
    talkgroups: set.talkgroups.filter((tg) => tg !== talkgroup),
  }))
}

function appendSortedGroup(groups: string[], group: string): string[] {
  if (groups.includes(group)) return groups
  return [...groups, group].sort((a, b) => a.localeCompare(b))
}

function SignalForgeLogo({ className = '' }: Readonly<{ className?: string }>) {
  return (
    <svg className={className} viewBox="0 0 120 120" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden>
      <path d="M60,15L60,88" stroke="currentColor" strokeWidth="2.5" />
      <path d="M60,36L38,55" stroke="currentColor" strokeWidth="2" />
      <path d="M60,36L82,55" stroke="currentColor" strokeWidth="2" />
      <path d="M60,55L22,80" stroke="currentColor" strokeWidth="2" />
      <path d="M60,55L98,80" stroke="currentColor" strokeWidth="2" />
      <path d="M22,80L98,80" stroke="currentColor" strokeWidth="1.5" />
      <g transform="translate(0,8)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
      <g transform="translate(0,2)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
      <g transform="translate(0,-4)"><path d="M46,28C55.333,18.667 64.667,18.667 74,28" stroke="currentColor" strokeWidth="2" /></g>
    </svg>
  )
}

function getSourceRuntimeStatus(source: IngestionSource, nowUnix: number): SourceRuntimeStatus {
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
        title: `offline — ${source.errorCount} errors`,
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
      title: `live — ${source.errorCount} past errors`,
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

function App() {
  const [connected, setConnected] = useState(false)
  const [wsLastDisconnectUnix, setWSLastDisconnectUnix] = useState<number | null>(null)
  const [apiHealthy, setAPIHealthy] = useState(false)
  const [apiLastHealthyUnix, setAPILastHealthyUnix] = useState<number | null>(null)
  const [calls, setCalls] = useState<Call[]>([])
  const [serverResults, setServerResults] = useState<Call[] | null>(null)
  const [serverLoading, setServerLoading] = useState(false)
  const [playingId, setPlayingId] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [groupFilter, setGroupFilter] = useState('')
  const [sortBy, setSortBy] = useState<'datetime' | 'duration' | 'frequency' | 'talkgroup'>('datetime')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')
  const [showFavoritesOnly, setShowFavoritesOnly] = useState(() => localStorage.getItem('p7_fav') === '1')
  const [hideMuted, setHideMuted] = useState(true)
  const [settingsMap, setSettingsMap] = useState<Record<number, TalkgroupSetting>>({})
  const [sourcesMap, setSourcesMap] = useState<Record<string, IngestionSource>>({})
  const [activeView, setActiveView] = useState<AppView>('monitor')
  const [talkgroupSearch, setTalkgroupSearch] = useState('')
  const [talkgroupActionID, setTalkgroupActionID] = useState<number | null>(null)
  const [newSourceID, setNewSourceID] = useState('')
  const [newSourceLabel, setNewSourceLabel] = useState('')
  const [newSourceError, setNewSourceError] = useState('')
  const [savingSourceID, setSavingSourceID] = useState<string | null>(null)
  const [deletingSourceID, setDeletingSourceID] = useState<string | null>(null)
  const [expandedSourceID, setExpandedSourceID] = useState<string | null>(null)
  const [sourceKeys, setSourceKeys] = useState<Record<string, SourceAPIKey[]>>({})
  const [sourceShares, setSourceShares] = useState<Record<string, string[]>>({})
  const [loadingKeysFor, setLoadingKeysFor] = useState<string | null>(null)
  const [generatingKeyFor, setGeneratingKeyFor] = useState<string | null>(null)
  const [loadingSharesFor, setLoadingSharesFor] = useState<string | null>(null)
  const [savingSharesFor, setSavingSharesFor] = useState<string | null>(null)
  const [nowUnix, setNowUnix] = useState(() => Math.floor(Date.now() / 1000))
  const [sourceStatusMap, setSourceStatusMap] = useState<Record<string, SmoothedSourceStatus>>({})
  const [editingSourceLabelID, setEditingSourceLabelID] = useState<string | null>(null)
  const [dirtySourceLabelMap, setDirtySourceLabelMap] = useState<Record<string, true>>({})
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null)
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResponse | null>(null)
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null)
  const [isStandalone, setIsStandalone] = useState(() => globalThis.matchMedia('(display-mode: standalone)').matches)
  const [authUser, setAuthUser] = useState<AuthUser | null>(null)
  const [authLoading, setAuthLoading] = useState(false)
  const [authEmail, setAuthEmail] = useState('')
  const [authToken, setAuthToken] = useState('')
  const [authMessage, setAuthMessage] = useState('')
  const [authError, setAuthError] = useState('')
  const [hubIdentity, setHubIdentity] = useState<HubIdentity | null>(null)
  const [hubDraft, setHubDraft] = useState<HubIdentityDraft>({ name: '', publicUrl: '', region: '', contact: '', federationEnabled: false })
  const [hubLoading, setHubLoading] = useState(false)
  const [hubMessage, setHubMessage] = useState('')
  const [hubError, setHubError] = useState('')
  const [hubInvites, setHubInvites] = useState<HubInvite[]>([])
  const [hubInviteActionID, setHubInviteActionID] = useState<string | null>(null)
  const [hubPeers, setHubPeers] = useState<HubPeer[]>([])
  const [hubPeerActionID, setHubPeerActionID] = useState<string | null>(null)
  const [peerRemoteURL, setPeerRemoteURL] = useState('')
  const [peerInviteToken, setPeerInviteToken] = useState('')
  const [sessionExpiresAt, setSessionExpiresAt] = useState<number | null>(null)
  const [sessionWarning, setSessionWarning] = useState('')
  const [users, setUsers] = useState<UserRecord[]>([])
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>([])
  const [auditLoading, setAuditLoading] = useState(false)
  const [usersLoading, setUsersLoading] = useState(false)
  const [userActionID, setUserActionID] = useState<string | null>(null)
  const [callPage, setCallPage] = useState(0)
  const [savedCallIds, setSavedCallIds] = useState<Set<number>>(new Set())
  const [radioSets, setRadioSets] = useState<RadioSet[]>([])
  const [selectedSetID, setSelectedSetID] = useState('')
  const [rsPlayingID, setRsPlayingID] = useState<string | null>(null)
  const [audioDevices, setAudioDevices] = useState<MediaDeviceInfo[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState('')
  const [distinctTalkgroups, setDistinctTalkgroups] = useState<TalkgroupInfo[]>([])
  const [rsName, setRsName] = useState('')
  const [rsCreateTGs, setRsCreateTGs] = useState<number[]>([])
  const [rsTGSearch, setRsTGSearch] = useState('')
  const [rsEditID, setRsEditID] = useState<string | null>(null)
  const [rsEditName, setRsEditName] = useState('')
  const [rsEditTGs, setRsEditTGs] = useState<number[]>([])
  const [rsError, setRsError] = useState('')
  const [rsLoading, setRsLoading] = useState(false)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  const refreshSources = () =>
    api.ingestionSources().then((sources) => {
      const next = Object.fromEntries(sources.map((source) => [source.id, source]))
      setSourcesMap((prev) => {
        for (const sourceID of Object.keys(dirtySourceLabelMap)) {
          if (!prev[sourceID] || !next[sourceID]) {
            continue
          }
          next[sourceID] = {
            ...next[sourceID],
            label: prev[sourceID].label,
          }
        }
        if (editingSourceLabelID && prev[editingSourceLabelID] && next[editingSourceLabelID]) {
          next[editingSourceLabelID] = {
            ...next[editingSourceLabelID],
            label: prev[editingSourceLabelID].label,
          }
        }
        return next
      })
    })

  const refreshAPIHealth = () =>
    api.health()
      .then(() => {
        setAPIHealthy(true)
        setAPILastHealthyUnix(Math.floor(Date.now() / 1000))
      })
      .catch(() => setAPIHealthy(false))

  const refreshAuthSession = () =>
  api.me()
    .then((result) => {
    setAuthUser(result.user)
    setSessionExpiresAt(result.sessionExpiresAt || null)
    })
    .catch((err) => {
    if (err instanceof ApiError && err.status === 401) {
      setAuthUser(null)
      setSessionExpiresAt(null)
      return
    }
    console.error(err)
    })

  const requireSignedIn = (reason: string): boolean => {
  if (authUser) {
    return true
  }
  setActiveView('account')
  setAuthError(reason)
  return false
  }

  const requireSourceWriteAccess = (reason: string): boolean => {
  if (!requireSignedIn(reason)) {
    return false
  }
  if (authUser?.role === 'guest') {
    setActiveView('account')
    setAuthError('Guest role is read-only for source management')
    return false
  }
  return true
  }

  const refreshUsers = () => {
  if (authUser?.role !== 'admin') {
    setUsers([])
    return Promise.resolve()
  }
  setUsersLoading(true)
  return api.users()
    .then((rows) => setUsers(rows))
    .catch((err) => {
    console.error(err)
    setAuthError(getErrorMessage(err, 'Could not load users'))
    })
    .finally(() => setUsersLoading(false))
  }

  const mergeCalls = (existing: Call[], incoming: Call[]): Call[] => {
  const merged = [...existing]
  const seen = new Set(existing.map((call) => call.id))
  for (const call of incoming) {
    if (seen.has(call.id)) continue
    merged.push(call)
    seen.add(call.id)
  }
  merged.sort((a, b) => b.dateTime - a.dateTime)
  return merged.slice(0, 500)
  }

  const refreshCalls = (limit = 250) =>
  api.calls({ limit, sort: 'datetime', order: 'desc' })
    .then((rows) => setCalls((prev) => mergeCalls(prev, rows)))
    .catch((err) => {
    console.error(err)
    })

  const refreshDistinctTalkgroups = () =>
    api.distinctTalkgroups()
      .then(setDistinctTalkgroups)
      .catch(console.error)

  const applyHubIdentity = (identity: HubIdentity) => {
    setHubIdentity(identity)
    setHubDraft({
      name: identity.name,
      publicUrl: identity.publicUrl,
      region: identity.region,
      contact: identity.contact,
      federationEnabled: identity.federationEnabled,
    })
  }

  const refreshHubIdentity = () => {
    setHubLoading(true)
    setHubError('')
    return api.hubIdentity()
      .then(applyHubIdentity)
      .catch((err) => {
        console.error(err)
        setHubError(getErrorMessage(err, 'Could not load hub identity'))
      })
      .finally(() => setHubLoading(false))
  }

  const refreshHubInvites = () => {
    if (authUser?.role !== 'admin') {
      setHubInvites([])
      return Promise.resolve()
    }
    return api.hubInvites()
      .then(setHubInvites)
      .catch((err) => {
        console.error(err)
        setHubError(getErrorMessage(err, 'Could not load hub invites'))
      })
  }

  const refreshHubPeers = () => {
    if (authUser?.role !== 'admin') {
      setHubPeers([])
      return Promise.resolve()
    }
    return api.hubPeers()
      .then(setHubPeers)
      .catch((err) => {
        console.error(err)
        setHubError(getErrorMessage(err, 'Could not load hub peers'))
      })
  }

  const refreshUpdateCheck = () =>
    api.updateCheck()
      .then(setUpdateInfo)
      .catch((err) => {
        console.error(err)
      })

  const refreshAuditLogs = (limit = 100) => {
  if (authUser?.role !== 'admin') {
    setAuditLogs([])
    return Promise.resolve()
  }
  setAuditLoading(true)
  return api.auditLogs(limit)
    .then((rows) => setAuditLogs(rows))
    .catch((err) => {
    console.error(err)
    setAuthError(getErrorMessage(err, 'Could not load audit logs'))
    })
    .finally(() => setAuditLoading(false))
  }

  // Load existing calls on mount
  useEffect(() => {
    Promise.all([
      api.calls({ limit: 500, sort: 'datetime', order: 'desc' }),
      api.talkgroupSettings(),
      api.callGroups(),
      refreshSources(),
      refreshAPIHealth(),
    ])
      .then(([callRows, settings, groups]) => {
        setCalls(callRows)
        const mapped = Object.fromEntries(settings.map((s) => [s.talkgroup, s]))
        setSettingsMap(mapped)
        setAllGroups(groups)
      })
      .catch(console.error)

    api.version()
      .then((info) => setVersionInfo(info))
      .catch(() => {
        // keep header usable even if version endpoint is briefly unavailable
      })

    refreshUpdateCheck()

  refreshAuthSession()
  }, [])

  useEffect(() => {
    if (!authUser) {
      setRadioSets([])
      setDistinctTalkgroups([])
      setHubIdentity(null)
      setHubInvites([])
      setHubPeers([])
      return
    }
    api.radioSets().then(setRadioSets).catch(console.error)
    refreshDistinctTalkgroups()
    refreshHubIdentity()
    refreshHubInvites()
    refreshHubPeers()
  }, [authUser])

  useEffect(() => {
  const token = new URLSearchParams(globalThis.window.location.search).get('token')
  if (!token) return

  setAuthToken(token)
  setAuthLoading(true)
  setAuthMessage('')
  setAuthError('')
  api.verifyMagicLink(token)
    .then((result) => {
    setAuthUser(result.user)
    setActiveView('account')
    setAuthMessage(`Signed in as ${result.user.email}`)
    globalThis.window.history.replaceState({}, '', globalThis.window.location.pathname)
    })
    .catch((err) => {
    setAuthError(getErrorMessage(err, 'Magic-link verification failed'))
    })
    .finally(() => setAuthLoading(false))
  }, [])

  useEffect(() => {
  if (activeView !== 'account') return
  if (authUser?.role !== 'admin') return
  refreshUsers()
  refreshAuditLogs()
  }, [activeView, authUser?.role])

  useEffect(() => {
  if (activeView !== 'integrations') return
  if (authUser?.role !== 'admin') return
  refreshUsers()
  }, [activeView, authUser?.role])

  useEffect(() => {
  if (connected) {
    refreshCalls(120)
  }
	const interval = setInterval(() => {
    refreshCalls(connected ? 60 : 120)
  }, connected ? 3000 : 4000)
	return () => clearInterval(interval)
  }, [connected])

  useEffect(() => {
  if (!authUser || !sessionExpiresAt) {
    setSessionWarning('')
    return
  }
  const remaining = sessionExpiresAt - nowUnix
  if (remaining <= 0) {
    setSessionWarning('Session expired. Please sign in again.')
    setAuthUser(null)
    setSessionExpiresAt(null)
    setUsers([])
    setActiveView('account')
    setAuthError('Your session expired. Please sign in again.')
    return
  }
  if (remaining <= SESSION_WARNING_WINDOW_SEC) {
    const mins = Math.max(1, Math.ceil(remaining / 60))
    setSessionWarning(`Session expires in ~${mins} minute${mins === 1 ? '' : 's'}. Save changes now.`)
    return
  }
  setSessionWarning('')
  }, [authUser, nowUnix, sessionExpiresAt])

  // Poll sources frequently as a safety net for metrics and status.
  useEffect(() => {
    refreshAPIHealth()

    const interval = setInterval(() => {
      refreshAPIHealth()
    }, 5000)
    return () => clearInterval(interval)
  }, [])

  useEffect(() => {
    refreshSources().catch(() => {
      // ignore transient network errors
    })

    const interval = setInterval(() => {
      refreshSources()
        .catch(() => { /* silently ignore polling errors */ })
    }, 2000)
    return () => clearInterval(interval)
  }, [dirtySourceLabelMap, editingSourceLabelID])

  useEffect(() => {
    const timer = setInterval(() => {
      setNowUnix(Math.floor(Date.now() / 1000))
    }, 1000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    const media = globalThis.matchMedia('(display-mode: standalone)')
    const updateStandalone = () => setIsStandalone(media.matches)
    const handleInstallPrompt = (event: Event) => {
      event.preventDefault()
      setInstallPrompt(event as BeforeInstallPromptEvent)
    }
    const handleInstalled = () => {
      setInstallPrompt(null)
      setIsStandalone(true)
    }

    updateStandalone()
    globalThis.addEventListener('beforeinstallprompt', handleInstallPrompt)
    globalThis.addEventListener('appinstalled', handleInstalled)
    media.addEventListener('change', updateStandalone)

    return () => {
      globalThis.removeEventListener('beforeinstallprompt', handleInstallPrompt)
      globalThis.removeEventListener('appinstalled', handleInstalled)
      media.removeEventListener('change', updateStandalone)
    }
  }, [])

  const installApp = async () => {
    if (!installPrompt) return
    await installPrompt.prompt()
    const choice = await installPrompt.userChoice
    if (choice.outcome !== 'dismissed') {
      setInstallPrompt(null)
    }
  }

  useEffect(() => {
    setSourceStatusMap((prev) => {
      const next: Record<string, SmoothedSourceStatus> = {}

      Object.values(sourcesMap).forEach((source) => {
        const rawStatus = getSourceRuntimeStatus(source, nowUnix)
        const existing = prev[source.id]

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

        // Prevent rapid live/offline flaps around stale thresholds and reconnect churn.
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
    })
  }, [nowUnix, sourcesMap])

  const ws = useMemo(
    () =>
      new WebSocketClient<WsEvent>({
        url: '/ws',
        reconnectIntervalMs: 1500,
        onConnect: () => {
          setConnected(true)
          setWSLastDisconnectUnix(null)
          refreshCalls(120)
        },
        onDisconnect: () => {
          setConnected(false)
          setWSLastDisconnectUnix(Math.floor(Date.now() / 1000))
        },
        onMessage: (msg) => {
          if (msg.type === 'call') {
            setCalls((prev) => mergeCalls(prev, [msg.call]))
            setDistinctTalkgroups((prev) => upsertTalkgroupInfo(prev, msg.call))
            if (msg.call.talkgroupGroup) {
              setAllGroups((prev) => appendSortedGroup(prev, msg.call.talkgroupGroup))
            }

            setRsPlayingID((activeID) => {
              if (activeID) {
                setRadioSets((sets) => {
                  const activeSet = sets.find((rs) => rs.id === activeID)
                  if (activeSet?.talkgroups.includes(msg.call.talkgroup)) {
                    setPlayingId((currentPlaying) => {
                      if (!currentPlaying) {
                        playCall(msg.call)
                      }
                      return currentPlaying
                    })
                  }
                  return sets
                })
              }
              return activeID
            })

            if (msg.sourceId) {
              // Keep source counters visually in sync in near real-time.
              setSourcesMap((prev) => {
                const existing = prev[msg.sourceId!]
                if (!existing) {
                  return prev
                }
                return {
                  ...prev,
                  [msg.sourceId!]: {
                    ...existing,
                    callsReceived: existing.callsReceived + 1,
                    lastSeenUnix: msg.call.dateTime,
                  },
                }
              })
            }

            return
          }

      if (msg.type === 'heartbeat') {
      return
      }

          if (msg.type === 'source_deleted') {
            setSourcesMap((prev) => {
              if (!prev[msg.sourceId]) return prev
              const next = { ...prev }
              delete next[msg.sourceId]
              return next
            })
            setSourceKeys((prev) => {
              if (!prev[msg.sourceId]) return prev
              const next = { ...prev }
              delete next[msg.sourceId]
              return next
            })
            setExpandedSourceID((current) => (current === msg.sourceId ? null : current))
          }
        },
      }),
    [],
  )

  useEffect(() => {
    ws.connect()
    return () => ws.disconnect()
  }, [ws])

  function playCall(call: Call) {
    if (call.redacted) {
      return
    }
    if (playingId === call.id) {
      audioRef.current?.pause()
      setPlayingId(null)
      return
    }
    const audio = audioRef.current ?? new Audio()
    audio.src = `/api/v1/calls/${call.id}/audio`
    audio.onended = () => setPlayingId(null)
    if (selectedDeviceId && 'setSinkId' in audio) {
      (audio as HTMLAudioElement & { setSinkId(id: string): Promise<void> })
        .setSinkId(selectedDeviceId)
        .catch(console.error)
    }
    audio.play().catch(console.error)
    audioRef.current = audio
    setPlayingId(call.id)
  }

  function enumerateAudioDevices() {
    navigator.mediaDevices?.enumerateDevices().then((devices) => {
      setAudioDevices(devices.filter((d) => d.kind === 'audiooutput'))
    }).catch(() => {})
  }

  async function updateTalkgroupSetting(
    talkgroup: number,
    update: (current: TalkgroupSetting | undefined) => TalkgroupSetting,
  ) {
    const current = settingsMap[talkgroup]
    const next = update(current)
    setSettingsMap((prev) => ({ ...prev, [talkgroup]: next }))
    try {
      const saved = await api.updateTalkgroupSettings(talkgroup, {
        favorite: next.favorite,
        muted: next.muted,
      })
      setSettingsMap((prev) => ({ ...prev, [talkgroup]: saved }))
    } catch (err) {
      console.error(err)
      setSettingsMap((prev) => ({ ...prev, [talkgroup]: current ?? next }))
    }
  }

  async function removeTalkgroup(talkgroup: number) {
    if (authUser?.role !== 'admin') return
    setTalkgroupActionID(talkgroup)
    try {
      await api.deleteTalkgroup(talkgroup)
      setCalls((prev) => prev.filter((call) => call.talkgroup !== talkgroup))
      setDistinctTalkgroups((prev) => prev.filter((tg) => tg.talkgroup !== talkgroup))
      setSettingsMap((prev) => {
        const next = { ...prev }
        delete next[talkgroup]
        return next
      })
      setRadioSets((prev) => removeTalkgroupFromRadioSets(prev, talkgroup))
    } catch (err) {
      console.error(err)
    } finally {
      setTalkgroupActionID(null)
    }
  }

  async function toggleSourceEnabled(source: IngestionSource) {
    if (!requireSourceWriteAccess('Sign in to manage sources')) {
      return
    }
    const updated = { ...source, enabled: !source.enabled }
    setSourcesMap((prev) => ({ ...prev, [source.id]: updated }))
    try {
      const saved = await api.updateIngestionSource(source.id, { enabled: !source.enabled })
      setSourcesMap((prev) => ({ ...prev, [source.id]: saved }))
    } catch (err) {
      console.error(err)
      setSourcesMap((prev) => ({ ...prev, [source.id]: source }))
    }
  }

  async function toggleSourceShared(source: IngestionSource, isShared: boolean) {
    if (authUser?.role !== 'admin') {
      return
    }
    setSourcesMap((prev) => ({ ...prev, [source.id]: { ...source, isShared } }))
    setSavingSourceID(source.id)
    try {
      const saved = await api.updateIngestionSource(source.id, { isShared })
      setSourcesMap((prev) => ({ ...prev, [source.id]: saved }))
    } catch (err) {
      console.error(err)
      setSourcesMap((prev) => ({ ...prev, [source.id]: source }))
    } finally {
      setSavingSourceID(null)
    }
  }

  async function saveSourceSettings(sourceId: string) {
    if (!requireSourceWriteAccess('Sign in to update source settings')) {
      return
    }
    const source = sourcesMap[sourceId]
    if (!source) {
      return
    }

    setSavingSourceID(source.id)
    try {
      const payload: Partial<IngestionSource> = {
        label: source.label,
        enabled: source.enabled,
      }
      if (authUser?.role === 'admin') {
        payload.isShared = source.isShared
      }
      const saved = await api.updateIngestionSource(source.id, payload)
      setSourcesMap((prev) => ({ ...prev, [source.id]: saved }))
      setDirtySourceLabelMap((prev) => {
        if (!prev[source.id]) {
          return prev
        }
        const next = { ...prev }
        delete next[source.id]
        return next
      })
      setEditingSourceLabelID((prev) => (prev === source.id ? null : prev))
    } catch (err) {
      console.error(err)
    } finally {
      setSavingSourceID(null)
    }
  }

  async function createSourceProfile() {
    if (!requireSourceWriteAccess('Sign in to create a source')) {
      return
    }
    const id = newSourceID.trim()
    if (!id) {
      setNewSourceError('Source ID is required')
      return
    }

    setNewSourceError('')
    setSavingSourceID(id)
    try {
      const saved = await api.updateIngestionSource(id, {
        label: newSourceLabel.trim(),
        enabled: true,
      })
      setSourcesMap((prev) => ({ ...prev, [saved.id]: saved }))
      setExpandedSourceID(saved.id)
      await generateKey(saved.id)
      setNewSourceID('')
      setNewSourceLabel('')
    } catch (err) {
      console.error(err)
      setNewSourceError('Could not create source profile')
    } finally {
      setSavingSourceID(null)
    }
  }

  async function loadSourceKeys(sourceId: string) {
    if (!requireSourceWriteAccess('Sign in to view source keys')) {
      return
    }
    setLoadingKeysFor(sourceId)
    try {
      const keys = await api.listSourceKeys(sourceId)
      setSourceKeys((prev) => ({ ...prev, [sourceId]: keys }))
    } catch (err) {
      console.error(err)
    } finally {
      setLoadingKeysFor(null)
    }
  }

  async function loadSourceShares(sourceId: string) {
    if (authUser?.role !== 'admin') {
      return
    }
    setLoadingSharesFor(sourceId)
    try {
      const shares = await api.sourceShares(sourceId)
      setSourceShares((prev) => ({ ...prev, [sourceId]: shares.userIds }))
    } catch (err) {
      console.error(err)
    } finally {
      setLoadingSharesFor(null)
    }
  }

  async function updateSourceShareUser(sourceId: string, userId: string, shared: boolean) {
    if (authUser?.role !== 'admin') {
      return
    }
    const current = sourceShares[sourceId] || []
    const next = shared ? Array.from(new Set([...current, userId])) : current.filter((id) => id !== userId)
    setSourceShares((prev) => ({ ...prev, [sourceId]: next }))
    setSavingSharesFor(sourceId)
    try {
      const saved = await api.updateSourceShares(sourceId, next)
      setSourceShares((prev) => ({ ...prev, [sourceId]: saved.userIds }))
    } catch (err) {
      console.error(err)
      setSourceShares((prev) => ({ ...prev, [sourceId]: current }))
    } finally {
      setSavingSharesFor(null)
    }
  }

  async function generateKey(sourceId: string) {
    if (!requireSourceWriteAccess('Sign in to generate source keys')) {
      return
    }
    setGeneratingKeyFor(sourceId)
    try {
      const newKey = await api.generateSourceKey(sourceId)
      setSourceKeys((prev) => ({
        ...prev,
        [sourceId]: [newKey, ...(prev[sourceId] || [])],
      }))
      setExpandedSourceID(sourceId)
    } catch (err) {
      console.error(err)
    } finally {
      setGeneratingKeyFor(null)
    }
  }

  async function revokeKey(sourceId: string, keyId: string) {
    if (!requireSourceWriteAccess('Sign in to revoke source keys')) {
      return
    }
    try {
      await api.revokeSourceKey(sourceId, keyId)
      setSourceKeys((prev) => ({
        ...prev,
        [sourceId]: (prev[sourceId] || []).filter((k) => k.id !== keyId),
      }))
    } catch (err) {
      console.error(err)
    }
  }

  async function toggleSourceKeyPanel(sourceId: string) {
    if (!requireSourceWriteAccess('Sign in to manage source keys')) {
      return
    }
    setExpandedSourceID((current) => (current === sourceId ? null : sourceId))
    if (!sourceKeys[sourceId]) {
      await loadSourceKeys(sourceId)
    }
    if (authUser?.role === 'admin' && !sourceShares[sourceId]) {
      await loadSourceShares(sourceId)
    }
  }

  async function deleteSource(source: IngestionSource) {
    if (!requireSourceWriteAccess('Sign in to delete sources')) {
      return
    }
    const sourceName = source.label || source.id
    const confirmed = globalThis.window.confirm(`Delete source "${sourceName}" and all associated API keys?`)
    if (!confirmed) {
      return
    }

    setDeletingSourceID(source.id)
    try {
      await api.deleteIngestionSource(source.id)
      setSourcesMap((prev) => {
        if (!prev[source.id]) return prev
        const next = { ...prev }
        delete next[source.id]
        return next
      })
      setSourceKeys((prev) => {
        if (!prev[source.id]) return prev
        const next = { ...prev }
        delete next[source.id]
        return next
      })
      setExpandedSourceID((current) => (current === source.id ? null : current))
    } catch (err) {
      console.error(err)
    } finally {
      setDeletingSourceID(null)
    }
  }

  async function requestMagicLink() {
  const email = authEmail.trim()
  if (!email) {
    setAuthError('Email is required')
    return
  }

  setAuthLoading(true)
  setAuthError('')
  setAuthMessage('')
  try {
    const result = await api.requestMagicLink(email)
    setAuthMessage(`Magic link issued for ${email}. Check inbox; in non-production token may be returned inline.`)
    if (result.token) {
      setAuthToken(result.token)
    }
  } catch (err) {
    setAuthError(getErrorMessage(err, 'Could not request magic link'))
  } finally {
    setAuthLoading(false)
  }
  }

  async function verifyMagicLinkToken() {
  const token = authToken.trim()
  if (!token) {
    setAuthError('Token is required')
    return
  }

  setAuthLoading(true)
  setAuthError('')
  setAuthMessage('')
  try {
    const result = await api.verifyMagicLink(token)
    setAuthUser(result.user)
    await refreshAuthSession()
    setAuthMessage(`Signed in as ${result.user.email}`)
    if (activeView === 'account' && result.user.role === 'admin') {
      await refreshUsers()
    }
  } catch (err) {
    setAuthError(getErrorMessage(err, 'Magic-link verification failed'))
  } finally {
    setAuthLoading(false)
  }
  }

  async function logoutSession() {
  setAuthLoading(true)
  setAuthError('')
  setAuthMessage('')
  try {
    await api.logout()
    setAuthUser(null)
    setSessionExpiresAt(null)
    setUsers([])
    setAuditLogs([])
    setAuthMessage('Logged out')
  } catch (err) {
    setAuthError(getErrorMessage(err, 'Could not logout'))
  } finally {
    setAuthLoading(false)
  }
  }

  async function saveUser(user: UserRecord) {
  setUserActionID(user.id)
  setAuthError('')
  try {
    await api.updateUser(user.id, { role: user.role, status: user.status })
    await refreshUsers()
  } catch (err) {
    setAuthError(getErrorMessage(err, `Could not update ${user.email}`))
  } finally {
    setUserActionID(null)
  }
  }

  async function removeUser(user: UserRecord) {
  const confirmed = globalThis.window.confirm(`Delete user ${user.email}?`)
  if (!confirmed) {
    return
  }
  setUserActionID(user.id)
  setAuthError('')
  try {
    await api.deleteUser(user.id)
    await refreshUsers()
  } catch (err) {
    setAuthError(getErrorMessage(err, `Could not delete ${user.email}`))
  } finally {
    setUserActionID(null)
  }
  }

  const [allGroups, setAllGroups] = useState<string[]>([])

  const filteredCalls = useMemo(() => {
    // When a search or favorites filter is active, use server results (full DB).
    // Otherwise use the in-memory live stream.
    let list = serverResults !== null ? [...serverResults] : [...calls]
    const q = search.trim().toLowerCase()
    if (q && serverResults === null) {
      // Fallback client-side filter only used while the server response is loading
      list = list.filter((call) =>
        [
          String(call.talkgroup),
          call.talkgroupLabel,
          call.talkgroupGroup,
          call.systemLabel,
          call.talkgroupTag,
        ]
          .join(' ')
          .toLowerCase()
          .includes(q),
      )
    }

    if (groupFilter && serverResults === null) {
      const g = groupFilter.toLowerCase()
      list = list.filter((call) => call.talkgroupGroup.toLowerCase().includes(g))
    }

    if (hideMuted) {
      list = list.filter((call) => !settingsMap[call.talkgroup]?.muted)
    }

    // Only apply client-side favorites filter when NOT using server results
    // (when using server results, the server already filtered by talkgroup IDs)
    if (showFavoritesOnly && serverResults === null) {
      list = list.filter((call) => settingsMap[call.talkgroup]?.favorite)
    }

    list.sort((a, b) => {
      const direction = sortOrder === 'asc' ? 1 : -1
      if (sortBy === 'datetime') return (a.dateTime - b.dateTime) * direction
      if (sortBy === 'duration') return (a.duration - b.duration) * direction
      if (sortBy === 'frequency') return (a.frequency - b.frequency) * direction
      return (a.talkgroup - b.talkgroup) * direction
    })
    return list
  }, [calls, serverResults, hideMuted, search, groupFilter, settingsMap, showFavoritesOnly, sortBy, sortOrder])

  useEffect(() => {
    setCallPage(0)
  }, [search, groupFilter, sortBy, sortOrder, showFavoritesOnly, hideMuted, selectedSetID])

  // Server-side query — fires 300 ms after the user stops typing or toggles favorites.
  // Queries the full DB instead of the capped in-memory stream.
  // Clears back to the live stream when both search and favorites are inactive.
  useEffect(() => {
    const q = search.trim()
    const favTalkgroups = showFavoritesOnly
      ? Object.entries(settingsMap)
          .filter(([, s]) => s.favorite)
          .map(([tg]) => Number(tg))
      : []

    const setTalkgroups = selectedSetID
      ? (radioSets.find((rs) => rs.id === selectedSetID)?.talkgroups ?? [])
      : []

    if (!q && !groupFilter && favTalkgroups.length === 0 && setTalkgroups.length === 0) {
      setServerResults(null)
      setServerLoading(false)
      return
    }
    // If favorites/group/set only (no text search), fire immediately; otherwise debounce
    const delay = q ? 300 : 0
    setServerLoading(true)
    const timer = setTimeout(() => {
      const params: { limit: number; sort: 'datetime'; order: 'desc'; q?: string; group?: string; talkgroups?: number[] } = { limit: 1000, sort: 'datetime', order: 'desc' }
      if (q) params.q = q
      if (groupFilter) params.group = groupFilter
      if (setTalkgroups.length > 0) {
        params.talkgroups = setTalkgroups
      } else if (favTalkgroups.length > 0) {
        params.talkgroups = favTalkgroups
      }
      api.calls(params)
        .then((rows) => setServerResults(rows))
        .catch(console.error)
        .finally(() => setServerLoading(false))
    }, delay)
    return () => clearTimeout(timer)
  }, [search, groupFilter, showFavoritesOnly, settingsMap, selectedSetID, radioSets])

  const pagedCalls = useMemo(
    () => filteredCalls.slice(callPage * CALL_PAGE_SIZE, (callPage + 1) * CALL_PAGE_SIZE),
    [filteredCalls, callPage],
  )
  const totalCallPages = Math.max(1, Math.ceil(filteredCalls.length / CALL_PAGE_SIZE))

  const allPageSelected = useMemo(
    () => pagedCalls.length > 0 && pagedCalls.every((c) => savedCallIds.has(c.id)),
    [pagedCalls, savedCallIds],
  )

  function toggleSelectPage() {
    if (allPageSelected) {
      setSavedCallIds((prev) => {
        const next = new Set(prev)
        pagedCalls.forEach((c) => next.delete(c.id))
        return next
      })
    } else {
      setSavedCallIds((prev) => {
        const next = new Set(prev)
        pagedCalls.forEach((c) => next.add(c.id))
        return next
      })
    }
  }

  async function saveHubIdentity() {
    if (authUser?.role !== 'admin') return
    setHubLoading(true)
    setHubMessage('')
    setHubError('')
    try {
      const saved = await api.updateHubIdentity(hubDraft)
      applyHubIdentity(saved)
      setHubMessage('Hub identity saved')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not save hub identity'))
    } finally {
      setHubLoading(false)
    }
  }

  async function createHubInvite() {
    if (authUser?.role !== 'admin') return
    setHubInviteActionID('new')
    setHubMessage('')
    setHubError('')
    try {
      const invite = await api.createHubInvite()
      setHubInvites((prev) => [invite, ...prev])
      copyToClipboard(invite.token)
      setHubMessage('Invite token created and copied')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not create hub invite'))
    } finally {
      setHubInviteActionID(null)
    }
  }

  async function revokeHubInvite(id: string) {
    if (authUser?.role !== 'admin') return
    setHubInviteActionID(id)
    setHubMessage('')
    setHubError('')
    try {
      const invite = await api.revokeHubInvite(id)
      setHubInvites((prev) => prev.map((row) => row.id === id ? invite : row))
      setHubMessage('Invite revoked')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not revoke hub invite'))
    } finally {
      setHubInviteActionID(null)
    }
  }

  async function connectHubPeer() {
    if (authUser?.role !== 'admin') return
    setHubPeerActionID('connect')
    setHubMessage('')
    setHubError('')
    try {
      const peer = await api.connectHubPeer(peerRemoteURL, peerInviteToken)
      setHubPeers((prev) => [peer, ...prev.filter((row) => row.hubId !== peer.hubId)])
      setPeerInviteToken('')
      setHubMessage('Peer hub connected')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not connect peer hub'))
    } finally {
      setHubPeerActionID(null)
    }
  }

  async function disableHubPeer(id: string) {
    if (authUser?.role !== 'admin') return
    setHubPeerActionID(id)
    setHubMessage('')
    setHubError('')
    try {
      const peer = await api.disableHubPeer(id)
      setHubPeers((prev) => prev.map((row) => row.id === id ? peer : row))
      setHubMessage('Peer disabled')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not disable peer'))
    } finally {
      setHubPeerActionID(null)
    }
  }

  async function downloadCallBox() {
    for (const id of Array.from(savedCallIds)) {
      const call = calls.find((c) => c.id === id)
      if (call?.redacted) {
        continue
      }
      const a = document.createElement('a')
      a.href = `/api/v1/calls/${id}/audio?download=1`
      a.download = call?.audioName || `call-${id}`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      await new Promise<void>((resolve) => globalThis.setTimeout(resolve, 500))
    }
  }

  function exportCSV() {
    const headers = ['ID', 'DateTime', 'System', 'SystemLabel', 'Talkgroup', 'TalkgroupLabel', 'TalkgroupGroup', 'TalkgroupTag', 'Frequency(Hz)', 'Duration(s)']
    const rows = filteredCalls.map((c) => [
      c.id,
      new Date(c.dateTime * 1000).toISOString(),
      c.system,
      c.systemLabel,
      c.talkgroup,
      c.talkgroupLabel,
      c.talkgroupGroup,
      c.talkgroupTag,
      c.frequency,
      c.duration.toFixed(1),
    ])
    const csv = [headers, ...rows]
      .map((row) => row.map((v) => `"${String(v).replace(/"/g, '""')}"`).join(','))
      .join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `calls-${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const talkgroupRows = useMemo(() => {
    const rows = new Map<number, {
      talkgroup: number
      label: string
      group: string
      system: string
      callCount: number
      lastSeen: number
      favorite: boolean
      muted: boolean
    }>()

    distinctTalkgroups.forEach((tg) => {
      const setting = settingsMap[tg.talkgroup]
      rows.set(tg.talkgroup, {
        talkgroup: tg.talkgroup,
        label: tg.talkgroupLabel,
        group: tg.talkgroupGroup,
        system: '',
        callCount: 0,
        lastSeen: 0,
        favorite: setting?.favorite ?? false,
        muted: setting?.muted ?? false,
      })
    })

    calls.forEach((call) => {
      const current = rows.get(call.talkgroup)
      const setting = settingsMap[call.talkgroup]
      if (!current) {
        rows.set(call.talkgroup, {
          talkgroup: call.talkgroup,
          label: call.talkgroupLabel,
          group: call.talkgroupGroup,
          system: call.systemLabel,
          callCount: 1,
          lastSeen: call.dateTime,
          favorite: setting?.favorite ?? false,
          muted: setting?.muted ?? false,
        })
        return
      }

      rows.set(call.talkgroup, {
        ...current,
        label: current.label || call.talkgroupLabel,
        group: current.group || call.talkgroupGroup,
        system: current.system || call.systemLabel,
        callCount: current.callCount + 1,
        lastSeen: Math.max(current.lastSeen, call.dateTime),
        favorite: setting?.favorite ?? current.favorite,
        muted: setting?.muted ?? current.muted,
      })
    })

    Object.values(settingsMap).forEach((setting) => {
      if (rows.has(setting.talkgroup)) {
        const current = rows.get(setting.talkgroup)
        if (!current) return
        rows.set(setting.talkgroup, {
          ...current,
          favorite: setting.favorite,
          muted: setting.muted,
        })
        return
      }

      rows.set(setting.talkgroup, {
        talkgroup: setting.talkgroup,
        label: '',
        group: '',
        system: '',
        callCount: 0,
        lastSeen: 0,
        favorite: setting.favorite,
        muted: setting.muted,
      })
    })

    const q = talkgroupSearch.trim().toLowerCase()
    return Array.from(rows.values())
      .filter((row) => {
        if (!q) return true
        return [String(row.talkgroup), row.label, row.group, row.system].join(' ').toLowerCase().includes(q)
      })
      .sort((a, b) => {
        if (b.callCount !== a.callCount) return b.callCount - a.callCount
        return b.lastSeen - a.lastSeen
      })
  }, [calls, distinctTalkgroups, settingsMap, talkgroupSearch])

  const uploadExample = useMemo(() => {
    const base = globalThis.window.location.origin
     return `curl -X POST "${base}/api/call-upload" \\\n  -F "key=your-api-key" \\\n  -F "system=1" -F "systemLabel=My System" \\\n  -F "talkgroup=1001" -F "talkgroupLabel=Dispatch" \\\n  -F "talkgroupGroup=FIRE" -F "talkgroupTag=PRIMARY" \\\n  -F "dateTime=$(date +%s)" -F "frequency=460325000" \\\n  -F "duration=3" -F "audioName=call.mp3" \\\n  -F "audioType=audio/mpeg" \\\n  -F "audio=@./call.mp3;type=audio/mpeg"`
  }, [])

  const overallStatus = useMemo(() => {
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

    // Guest sessions cannot open /ws by design, so websocket state should not render as an outage.
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

  const headerVersionLabel = useMemo(() => {
    return deploymentHeaderLabel(versionInfo)
  }, [versionInfo])

  const headerVersionTitle = useMemo(() => {
    return deploymentTitle(versionInfo)
  }, [versionInfo])

  const footerDeploymentLabel = useMemo(() => {
    return deploymentFooterLabel(versionInfo)
  }, [versionInfo])

  const footerSourceLinks = useMemo(() => {
    return sourceLinks(versionInfo)
  }, [versionInfo])

  const updateTitle = useMemo(() => {
    if (!updateInfo?.latest) return updateInfo?.error || 'update check unavailable'
    const latest = updateInfo.latest
    return [
      `latest tag: ${latest.imageTag || latest.shortCommit || 'unknown'}`,
      latest.commit ? `latest commit: ${latest.commit}` : '',
      latest.publishedAt ? `published: ${latest.publishedAt}` : '',
      `current tag: ${versionInfo?.deployTag || 'unknown'}`,
    ].filter(Boolean).join('\n')
  }, [updateInfo, versionInfo])

  let updateStatusLabel = 'current'
  if (updateInfo?.error) updateStatusLabel = 'check failed'
  if (updateInfo?.updateAvailable) updateStatusLabel = 'update available'
  const updateStatusClass = updateInfo?.updateAvailable ? 'text-console-amber' : 'text-console-accent'

  let pwaInstallControl: JSX.Element | null = null
  if (installPrompt && !isStandalone) {
    pwaInstallControl = (
      <button
        onClick={installApp}
        className="px-2 py-1 border border-console-accent text-console-accent rounded uppercase tracking-wider hover:bg-console-accent hover:bg-opacity-10"
        title="Install P7 Scanner as an app"
      >
        INSTALL APP
      </button>
    )
  } else if (isStandalone) {
    pwaInstallControl = <span className="console-label" title="running as an installed app">APP MODE</span>
  }

  return (
    <div className="min-h-screen bg-console-bg text-console-text font-mono px-3 py-3 sm:px-6 sm:py-5 flex flex-col gap-3 sm:gap-4 overflow-x-hidden">
      <header className="console-panel flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <SignalForgeLogo className="h-10 w-10 flex-shrink-0 text-white drop-shadow-[0_0_10px_rgba(0,255,65,0.18)]" />
          <div className="min-w-0">
            <div className="text-lg font-bold tracking-widest truncate">P7 // SCAN</div>
            <div className="console-label text-[9px]">SIGNALFORGE NODE CONSOLE</div>
          </div>
        </div>
        <div className="flex items-center gap-2 sm:gap-4 text-xs flex-wrap min-w-0">
          <span className="console-label" title={headerVersionTitle}>{headerVersionLabel}</span>
          {updateInfo?.updateAvailable && (
            <button
              onClick={() => setActiveView('hub')}
              className="px-2 py-1 border border-console-amber text-console-amber rounded uppercase tracking-wider hover:bg-console-amber hover:bg-opacity-10"
              title={updateTitle}
            >
              UPDATE AVAILABLE
            </button>
          )}
          {pwaInstallControl}
          <span className="flex items-center gap-2">
            <span
              className={`inline-block h-2.5 w-2.5 rounded-full ${overallStatus.dotClass}`}
              aria-hidden
              title={overallStatus.title}
            />
            {overallStatus.label}
          </span>
          <span 
            className={`flex items-center gap-1 px-2 py-1 border rounded min-w-0 max-w-full ${authUser ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
            title={authUser ? `Logged in as ${authUser.email}` : 'Not logged in'}
          >
            <span>{authUser ? '[●]' : '[○]'}</span>
            <span className="text-[10px] truncate">{authUser ? authUser.email : 'GUEST'}</span>
          </span>
        </div>
      </header>

      <div className="console-panel grid grid-cols-2 gap-2 text-xs sm:flex sm:items-center sm:flex-wrap">
        {(['monitor', 'radio-sets', 'integrations', 'talkgroups', 'hub', 'account'] as AppView[]).map((view) => {
          const viewLabel: Record<AppView, string> = { monitor: 'CALL LOG', 'radio-sets': 'RADIO SETS', integrations: 'INTEGRATIONS', talkgroups: 'TALKGROUPS', hub: 'HUB', account: 'ACCOUNT' }
          const isAvailable = authUser || view === 'account'
          const isAccountWhenLoggedOut = view === 'account' && !authUser
          return (
            <button
              key={view}
              onClick={() => isAvailable && setActiveView(view)}
              className={`min-w-0 px-2.5 py-1 border rounded uppercase tracking-wider transition-all text-[10px] sm:text-xs ${
                isAccountWhenLoggedOut
                  ? 'border-console-accent text-console-accent bg-console-accent bg-opacity-5 account-tab-attn font-bold'
                  : !isAvailable
                  ? 'border-console-border text-console-muted opacity-50 cursor-not-allowed'
                  : activeView === view
                  ? 'border-console-accent text-console-accent bg-console-surface'
                  : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
              }`}
              disabled={!isAvailable}
            >
              {viewLabel[view]}
            </button>
          )
        })}
      </div>

    {sessionWarning && (
    <div className="console-panel border border-console-error text-console-error text-xs px-3 py-2">
      <div className="flex items-center justify-between gap-3">
      <span>{sessionWarning}</span>
      <button
        onClick={() => setActiveView('account')}
        className="px-2 py-1 border border-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
      >
        RE-AUTH
      </button>
      </div>
    </div>
    )}

    {!authUser && (
    <div className="console-panel border border-console-border text-console-accent text-xs px-4 py-6 sm:px-6 sm:py-8">
      <div className="mx-auto max-w-3xl text-center space-y-4">
        <SignalForgeLogo className="mx-auto h-24 w-24 text-white drop-shadow-[0_0_16px_rgba(0,255,65,0.2)]" />
        <div className="space-y-2">
          <p className="text-sm font-bold tracking-widest">[ P7 SCANNER ]</p>
          <p className="text-console-muted text-[11px] leading-6">
            Private console for monitoring, organizing, and sharing community radio traffic through SignalForge.
          </p>
          <p className="text-console-muted text-[11px] leading-6">
            Sign in to manage sources, radio sets, talkgroups, and live call playback. Recorder downloads are published publicly through SignalForge.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row sm:justify-center">
          <button
            onClick={() => setActiveView('account')}
            className="px-3 py-2 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10"
          >
            ACCOUNT SIGN IN
          </button>
          <a
            href="https://signalforge.org/#recorder"
            className="px-3 py-2 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-widest hover:border-console-accent hover:text-console-accent"
          >
            RECORDER DOWNLOADS
          </a>
        </div>
      </div>
    </div>
    )}

      {authUser && activeView === 'monitor' && (
        <div className="flex flex-col gap-3">
          {/* Sources strip */}
          <div className="console-panel">
            <div className="flex items-center gap-2 sm:gap-3 flex-wrap">
              <span className="console-label text-[10px] flex-shrink-0">SOURCES</span>
              {Object.values(sourcesMap).map((source) => {
              const canManageThisSource = authUser?.role === 'admin' || source.userId === authUser?.id
                const status = sourceStatusMap[source.id] ?? {
                  ...getSourceRuntimeStatus(source, nowUnix),
                  changedAtUnix: nowUnix,
                }
                return (
                  <div
                    key={source.id}
                    className="flex items-center gap-1.5 border border-console-border rounded px-2 py-1 text-[10px] min-w-0 max-w-full"
                  >
                    <span className={`h-1.5 w-1.5 rounded-full flex-shrink-0 ${status.dotClass}`} aria-hidden title={status.title} />
                    <span className="font-semibold truncate max-w-[120px]" title={source.label || source.id}>{source.label || source.id}</span>
                    <span className="text-console-muted tabular-nums">{source.callsReceived}c</span>
                    {source.errorCount > 0 && <span className="text-console-error tabular-nums">{source.errorCount}e</span>}
                    <button
                      onClick={() => toggleSourceEnabled(source)}
                      disabled={!canManageThisSource}
                      className={`px-1.5 py-0.5 border rounded text-[9px] transition-colors whitespace-nowrap ${
                        source.enabled
                          ? 'border-console-accent text-console-accent hover:bg-console-accent hover:bg-opacity-10'
                          : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
                      }`}
                      title={source.enabled ? 'set source to disabled' : 'set source to enabled'}
                    >
                      {source.enabled ? 'ON' : 'OFF'}
                    </button>
                  </div>
                )
              })}
              {Object.keys(sourcesMap).length === 0 && (
                <span className="text-console-muted text-[10px]">No sources configured</span>
              )}
            </div>
          </div>

          <main className="console-panel overflow-hidden flex flex-col">
            <div className="flex items-center justify-between gap-2 flex-wrap mb-3">
              <span className="console-label text-xs">CALL LOG</span>
              <span className="text-xs text-console-muted tabular-nums">
                {serverLoading
                  ? 'searching…'
                  : serverResults !== null
                    ? `${filteredCalls.length} results`
                    : `${filteredCalls.length}/${calls.length} calls`}
                {totalCallPages > 1 && (
                  <span className="ml-2 text-[10px]">· pg {callPage + 1}/{totalCallPages}</span>
                )}
              </span>
            </div>

            <div className="mb-3 grid gap-2 md:grid-cols-[2fr_1fr_1fr_1fr_1fr_auto_auto_auto]">
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search talkgroup, system, tag..."
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
              />
              <select
                value={groupFilter}
                onChange={(e) => setGroupFilter(e.target.value)}
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
              >
                <option value="">Group: All</option>
                {allGroups.map((g) => (
                  <option key={g} value={g}>{g}</option>
                ))}
              </select>
              <select
                value={selectedSetID}
                onChange={(e) => setSelectedSetID(e.target.value)}
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
              >
                <option value="">Set: All</option>
                {radioSets.map((rs) => (
                  <option key={rs.id} value={rs.id}>{rs.name}</option>
                ))}
              </select>
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as 'datetime' | 'duration' | 'frequency' | 'talkgroup')}
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
              >
                <option value="datetime">Sort: Time</option>
                <option value="talkgroup">Sort: Talkgroup</option>
                <option value="frequency">Sort: Frequency</option>
                <option value="duration">Sort: Duration</option>
              </select>
              <select
                value={sortOrder}
                onChange={(e) => setSortOrder(e.target.value as 'asc' | 'desc')}
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
              >
                <option value="desc">Order: Desc</option>
                <option value="asc">Order: Asc</option>
              </select>

              <label className="text-xs flex items-center gap-2 cursor-pointer min-w-0">
                <input
                  type="checkbox"
                  checked={showFavoritesOnly}
                  onChange={(e) => {
                    setShowFavoritesOnly(e.target.checked)
                    localStorage.setItem('p7_fav', e.target.checked ? '1' : '0')
                  }}
                />
                <span>Favorites only</span>
              </label>
              <label className="text-xs flex items-center gap-2 cursor-pointer min-w-0">
                <input type="checkbox" checked={hideMuted} onChange={(e) => setHideMuted(e.target.checked)} />
                <span>Hide muted</span>
              </label>
              <button
                onClick={exportCSV}
                title="Export current filtered calls as CSV"
                className="px-2 py-1 border border-console-border rounded text-[10px] uppercase tracking-widest text-console-muted hover:border-console-accent hover:text-console-accent"
              >
                CSV
              </button>
            </div>

            {savedCallIds.size > 0 && (
              <div className="mb-2 flex items-center gap-2 sm:gap-3 flex-wrap px-2 py-1.5 border border-console-accent/50 rounded text-[10px]">
                <span className="text-console-accent font-semibold">[■] CALL BOX</span>
                <span className="text-console-muted">
                  {savedCallIds.size} call{savedCallIds.size !== 1 ? 's' : ''} saved
                </span>
                <div className="hidden sm:block flex-1" />
                <button
                  onClick={downloadCallBox}
                  className="px-2 py-0.5 border border-console-accent text-console-accent rounded hover:bg-console-accent/10"
                >
                  DOWNLOAD ALL
                </button>
                <button
                  onClick={() => setSavedCallIds(new Set())}
                  className="px-2 py-0.5 border border-console-border text-console-muted rounded hover:border-console-accent hover:text-console-accent"
                >
                  CLEAR
                </button>
              </div>
            )}

            {filteredCalls.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-center py-16">
                <div>
                  <p className="console-label">SYSTEM READY</p>
                  <p className="mt-3 text-2xl console-accent">No calls match current filters</p>
                  <span className="cursor-blink ml-1" aria-hidden>
                    █
                  </span>
                </div>
              </div>
            ) : (
              <div className="overflow-auto flex-1">
                <table className="w-full border-collapse">
                  <thead>
                    <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
                      <th className="py-1.5 px-2 text-left font-normal w-6">
                        <input
                          type="checkbox"
                          checked={allPageSelected}
                          onChange={toggleSelectPage}
                          title="Select all on this page"
                        />
                      </th>
                      <th className="py-1.5 px-3 text-left font-normal">Time</th>
                      <th className="py-1.5 px-3 text-left font-normal">TG</th>
                      <th className="py-1.5 px-3 text-left font-normal">Label</th>
                      <th className="py-1.5 px-3 text-left font-normal">Group</th>
                      <th className="py-1.5 px-3 text-left font-normal">System</th>
                      <th className="py-1.5 px-3 text-left font-normal">Frequency</th>
                      <th className="py-1.5 px-3 text-left font-normal">Dur</th>
                      <th className="py-1.5 px-3 text-left font-normal">Audio</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedCalls.map((call) => (
                      <CallRow
                        key={call.id}
                        call={call}
                        playing={playingId === call.id}
                        favorite={settingsMap[call.talkgroup]?.favorite ?? false}
                        muted={settingsMap[call.talkgroup]?.muted ?? false}
                        saved={savedCallIds.has(call.id)}
                        onPlay={() => playCall(call)}
                        onToggleFavorite={() =>
                          updateTalkgroupSetting(call.talkgroup, (current) => ({
                            talkgroup: call.talkgroup,
                            favorite: !(current?.favorite ?? false),
                            muted: current?.muted ?? false,
                            updatedAt: Date.now() / 1000,
                          }))
                        }
                        onToggleMuted={() =>
                          updateTalkgroupSetting(call.talkgroup, (current) => ({
                            talkgroup: call.talkgroup,
                            favorite: current?.favorite ?? false,
                            muted: !(current?.muted ?? false),
                            updatedAt: Date.now() / 1000,
                          }))
                        }
                        onToggleSaved={() =>
                          setSavedCallIds((prev) => {
                            const next = new Set(prev)
                            if (next.has(call.id)) {
                              next.delete(call.id)
                            } else {
                              next.add(call.id)
                            }
                            return next
                          })
                        }
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {totalCallPages > 1 && (
              <div className="flex items-center justify-between gap-2 flex-wrap border-t border-console-border pt-2 mt-1">
                <button
                  onClick={() => setCallPage((p) => Math.max(0, p - 1))}
                  disabled={callPage === 0}
                  className="px-2 py-1 border border-console-border rounded text-[10px] text-console-muted hover:border-console-accent hover:text-console-accent disabled:opacity-30 disabled:cursor-not-allowed"
                >
                  ← PREV
                </button>
                <span className="text-[10px] text-console-muted tabular-nums">
                  {callPage * CALL_PAGE_SIZE + 1}–{Math.min((callPage + 1) * CALL_PAGE_SIZE, filteredCalls.length)} of {filteredCalls.length}
                </span>
                <button
                  onClick={() => setCallPage((p) => Math.min(totalCallPages - 1, p + 1))}
                  disabled={callPage >= totalCallPages - 1}
                  className="px-2 py-1 border border-console-border rounded text-[10px] text-console-muted hover:border-console-accent hover:text-console-accent disabled:opacity-30 disabled:cursor-not-allowed"
                >
                  NEXT →
                </button>
              </div>
            )}
          </main>
        </div>
      )}

      {authUser && activeView === 'radio-sets' && (
        <main className="console-panel flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <span className="console-label text-xs">RADIO SETS</span>
            <span className="text-xs text-console-muted tabular-nums">{radioSets.length} sets</span>
          </div>

          {rsError && (
            <p className="text-xs text-console-error border border-console-error rounded px-2 py-1">{rsError}</p>
          )}

          <div className="border border-console-border rounded p-3 flex flex-col gap-3">
            <p className="console-label text-xs">{rsEditID ? 'EDIT RADIO SET' : 'CREATE RADIO SET'}</p>
            <input
              value={rsEditID ? rsEditName : rsName}
              onChange={(e) => rsEditID ? setRsEditName(e.target.value) : setRsName(e.target.value)}
              placeholder="Set name..."
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
            />
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <span className="text-[10px] text-console-muted uppercase tracking-wider">Talkgroups</span>
                <input
                  value={rsTGSearch}
                  onChange={(e) => setRsTGSearch(e.target.value)}
                  placeholder="Filter..."
                  className="bg-console-bg border border-console-border rounded px-2 py-0.5 text-[10px] outline-none focus:border-console-accent w-full sm:w-32"
                />
              </div>
              <div className="max-h-48 overflow-y-auto border border-console-border rounded divide-y divide-console-border/50">
                {distinctTalkgroups
                  .filter((tg) => {
                    const q = rsTGSearch.trim().toLowerCase()
                    if (!q) return true
                    return [String(tg.talkgroup), tg.talkgroupLabel, tg.talkgroupGroup].join(' ').toLowerCase().includes(q)
                  })
                  .map((tg) => {
                    const selectedTGs = rsEditID ? rsEditTGs : rsCreateTGs
                    const setSelected = rsEditID ? setRsEditTGs : setRsCreateTGs
                    const checked = selectedTGs.includes(tg.talkgroup)
                    return (
                      <label key={tg.talkgroup} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() =>
                            setSelected((prev) =>
                              checked ? prev.filter((id) => id !== tg.talkgroup) : [...prev, tg.talkgroup]
                            )
                          }
                        />
                        <span className="text-console-accent tabular-nums">{tg.talkgroup}</span>
                        {tg.talkgroupLabel && <span>{tg.talkgroupLabel}</span>}
                        {tg.talkgroupGroup && <span className="text-console-muted">{tg.talkgroupGroup}</span>}
                      </label>
                    )
                  })}
                {distinctTalkgroups.length === 0 && (
                  <p className="text-[10px] text-console-muted px-2 py-2">No talkgroups seen yet</p>
                )}
              </div>
            </div>
            <div className="flex gap-2 flex-wrap">
              <button
                onClick={async () => {
                  if (rsEditID) {
                    const name = rsEditName.trim()
                    if (!name) { setRsError('Name is required'); return }
                    setRsLoading(true); setRsError('')
                    try {
                      const updated = await api.updateRadioSet(rsEditID, name, rsEditTGs)
                      setRadioSets((prev) => prev.map((rs) => rs.id === rsEditID ? updated : rs))
                      setRsEditID(null); setRsEditName(''); setRsEditTGs([])
                    } catch (err) { setRsError('Could not update radio set') }
                    finally { setRsLoading(false) }
                  } else {
                    const name = rsName.trim()
                    if (!name) { setRsError('Name is required'); return }
                    setRsLoading(true); setRsError('')
                    try {
                      const created = await api.createRadioSet(name, rsCreateTGs)
                      setRadioSets((prev) => [...prev, created])
                      setRsName(''); setRsCreateTGs([])
                    } catch (err) { setRsError('Could not create radio set') }
                    finally { setRsLoading(false) }
                  }
                }}
                disabled={rsLoading}
                className="px-3 py-1 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50 flex-1 sm:flex-none"
              >
                {rsLoading ? 'SAVING...' : rsEditID ? 'SAVE' : 'CREATE'}
              </button>
              {rsEditID && (
                <button
                  onClick={() => { setRsEditID(null); setRsEditName(''); setRsEditTGs([]) }}
                  className="px-3 py-1 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-widest hover:border-console-accent hover:text-console-accent flex-1 sm:flex-none"
                >
                  CANCEL
                </button>
              )}
            </div>
          </div>

          {radioSets.length === 0 ? (
            <p className="text-xs text-console-muted">No radio sets created yet</p>
          ) : (
            <div className="flex flex-col gap-2">
              {radioSets.map((rs) => {
                const canManageRadioSet = rs.userId === authUser?.id
                return (
                <div key={rs.id} className="border border-console-border rounded p-2.5 flex flex-col gap-2">
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex flex-col gap-0.5 min-w-0">
                      <span className="text-xs font-semibold">{rs.name}</span>
                      <span className="text-[10px] text-console-muted">{rs.talkgroups.length} talkgroup{rs.talkgroups.length !== 1 ? 's' : ''}</span>
                      {authUser?.role === 'admin' && (
                        <span className="text-[10px] text-console-muted">
                          owner {rs.userId || 'unknown'} | sources {(rs.sourceIds && rs.sourceIds.length > 0) ? rs.sourceIds.join(', ') : 'none'}
                        </span>
                      )}
                    </div>
                    <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-shrink-0 sm:flex-wrap sm:justify-end">
                      <button
                        onClick={() => {
                          setRsPlayingID((prev) => {
                            const next = prev === rs.id ? null : rs.id
                            if (next) enumerateAudioDevices()
                            return next
                          })
                        }}
                        className={`px-2 py-1 sm:py-0.5 border rounded text-[10px] ${
                          rsPlayingID === rs.id
                            ? 'border-console-accent text-console-accent bg-console-accent/10'
                            : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
                        }`}
                      >
                        {rsPlayingID === rs.id ? '■ SCANNING' : '▶ SCAN'}
                      </button>
                      {rsPlayingID === rs.id && audioDevices.length > 0 && (
                        <select
                          value={selectedDeviceId}
                          onChange={(e) => {
                            const id = e.target.value
                            setSelectedDeviceId(id)
                            if (audioRef.current && 'setSinkId' in audioRef.current) {
                              (audioRef.current as HTMLAudioElement & { setSinkId(id: string): Promise<void> })
                                .setSinkId(id)
                                .catch(console.error)
                            }
                          }}
                          className="col-span-2 bg-console-bg border border-console-border rounded px-2 py-1 sm:py-0.5 text-[10px] text-console-muted outline-none focus:border-console-accent min-w-0"
                          title="Audio output device"
                        >
                          <option value="">Output: Default</option>
                          {audioDevices.map((d) => (
                            <option key={d.deviceId} value={d.deviceId}>
                              {d.label || `Device ${d.deviceId.slice(0, 6)}`}
                            </option>
                          ))}
                        </select>
                      )}
                      <button
                        onClick={async () => {
                          try {
                            const updated = await api.generateShareToken(rs.id)
                            setRadioSets((prev) => prev.map((r) => r.id === rs.id ? updated : r))
                          } catch { setRsError('Could not generate share link') }
                        }}
                        className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                        disabled={!canManageRadioSet}
                      >
                        SHARE
                      </button>
                      <button
                        onClick={() => {
                          if (!rs.shareToken) return
                          globalThis.window.open(`/public/player/${rs.shareToken}`, '_blank', 'noopener,noreferrer')
                        }}
                        className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-30 disabled:cursor-not-allowed"
                        disabled={!rs.shareToken}
                        title={rs.shareToken ? 'Open public player' : 'Generate a share link first'}
                      >
                        OPEN
                      </button>
                      <button
                        onClick={() => {
                          setRsEditID(rs.id)
                          setRsEditName(rs.name)
                          setRsEditTGs([...rs.talkgroups])
                        }}
                        className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                        disabled={!canManageRadioSet}
                      >
                        EDIT
                      </button>
                      <button
                        onClick={async () => {
                          if (!globalThis.window.confirm(`Delete radio set "${rs.name}"?`)) return
                          try {
                            await api.deleteRadioSet(rs.id)
                            setRadioSets((prev) => prev.filter((r) => r.id !== rs.id))
                            if (selectedSetID === rs.id) setSelectedSetID('')
                            if (rsPlayingID === rs.id) setRsPlayingID(null)
                          } catch { setRsError('Could not delete radio set') }
                        }}
                        className="px-2 py-1 sm:py-0.5 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                        disabled={!canManageRadioSet}
                      >
                        DEL
                      </button>
                    </div>
                  </div>

                  {rs.shareToken && (
                    <div className="border border-console-border/50 rounded p-2 flex flex-col gap-1.5 bg-console-bg/40">
                      <div className="flex items-center justify-between gap-2 flex-wrap">
                        <span className="text-[10px] text-console-muted uppercase tracking-wider">Share links</span>
                        <button
                          onClick={async () => {
                            try {
                              const updated = await api.revokeShareToken(rs.id)
                              setRadioSets((prev) => prev.map((r) => r.id === rs.id ? updated : r))
                            } catch { setRsError('Could not revoke share link') }
                          }}
                          className="px-1.5 py-0.5 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                          disabled={!canManageRadioSet}
                        >
                          REVOKE
                        </button>
                      </div>
                      {[
                        { label: 'Player', url: `/public/player/${rs.shareToken}` },
                      ].map(({ label, url }) => (
                        <div key={label} className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
                          <span className="text-[10px] text-console-muted w-10 flex-shrink-0">{label}</span>
                          <input
                            readOnly
                            value={`${globalThis.window.location.origin}${url}`}
                            onClick={(e) => (e.target as HTMLInputElement).select()}
                            className="flex-1 bg-console-bg border border-console-border/50 rounded px-1.5 py-0.5 text-[10px] text-console-accent outline-none min-w-0"
                          />
                          <button
                            onClick={() =>
                              navigator.clipboard?.writeText(`${globalThis.window.location.origin}${url}`)
                            }
                            className="px-1.5 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent flex-shrink-0"
                          >
                            COPY
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
                )
              })}
            </div>
          )}
        </main>
      )}

      {authUser && activeView === 'integrations' && (
        <main className="console-panel flex flex-col gap-4">
          <div className="grid gap-3 md:grid-cols-[2fr_1fr]">
            <div className="border border-console-border rounded p-3">
              <p className="console-label text-xs mb-2">CALL UPLOAD ENDPOINT</p>
              <p className="text-xs text-console-muted break-all">
                {`${globalThis.window.location.origin}/api/call-upload`}
              </p>
              <p className="text-[11px] text-console-muted mt-2">
                Use the source&apos;s API key in the key field. This is compatible with SDRTrunk-style multipart uploads.
              </p>
            </div>
            <div className="border border-console-border rounded p-3">
              <p className="console-label text-xs mb-2">ADD SOURCE PROFILE</p>
              <div className="flex flex-col gap-2">
                <input
                  value={newSourceID}
                  onChange={(e) => setNewSourceID(e.target.value)}
                  placeholder="source id"
                  className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                />
                <input
                  value={newSourceLabel}
                  onChange={(e) => setNewSourceLabel(e.target.value)}
                  placeholder="source label"
                  className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                />
                <button
                  onClick={createSourceProfile}
                  className="px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10"
                >
                  CREATE SOURCE
                </button>
                {newSourceError && <p className="text-[11px] text-console-error">{newSourceError}</p>}
              </div>
            </div>
          </div>

          <div className="border border-console-border rounded p-3 overflow-auto">
            <p className="console-label text-xs mb-2">INGESTION SOURCE SETTINGS</p>
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
                  <th className="py-1.5 px-2 text-left font-normal">Source</th>
                  <th className="py-1.5 px-2 text-left font-normal">Label</th>
                  <th className="py-1.5 px-2 text-left font-normal">Enabled</th>
                  <th className="py-1.5 px-2 text-left font-normal">Shared</th>
                  <th className="py-1.5 px-2 text-left font-normal">API Keys</th>
                  <th className="py-1.5 px-2 text-left font-normal">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Object.values(sourcesMap).map((source) => {
                  const keysForSource = sourceKeys[source.id] || []
                  const canManageThisSource = authUser?.role === 'admin' || source.userId === authUser?.id
                  let sourceShareStatus = `${sourceShares[source.id]?.length || 0} selected`
                  if (savingSharesFor === source.id) {
                    sourceShareStatus = 'SAVING...'
                  } else if (loadingSharesFor === source.id) {
                    sourceShareStatus = 'LOADING...'
                  }
                  let keyPanel: JSX.Element

                  if (loadingKeysFor === source.id) {
                    keyPanel = <div className="text-console-muted">Loading keys...</div>
                  } else if (keysForSource.length === 0) {
                    keyPanel = <div className="text-console-muted">No keys yet</div>
                  } else {
                    keyPanel = (
                      <div className="flex flex-col gap-2">
                        {keysForSource.map((key) => (
                          <div
                            key={key.id}
                            className="flex items-center justify-between gap-3 rounded border border-console-border px-2 py-1"
                          >
                            <div className="min-w-0 flex-1">
                              <div className="text-console-text break-all">{redactKey(key.apiKey)}</div>
                              <div className="text-[10px] text-console-muted tabular-nums">
                                created {fmtTime(key.createdAt)}
                                {key.lastUsedAt > 0 && ` · last used ${fmtTime(key.lastUsedAt)}`}
                              </div>
                            </div>
                            <div className="flex items-center gap-2 flex-shrink-0">
                              <button
                                onClick={() => copyToClipboard(key.apiKey)}
                                className="px-2 py-1 border border-console-border rounded text-[10px] text-console-muted hover:border-console-accent hover:text-console-accent"
                              >
                                COPY
                              </button>
                              <button
                                onClick={() => revokeKey(source.id, key.id)}
                                className="px-2 py-1 border border-console-error rounded text-[10px] text-console-error hover:bg-console-error hover:bg-opacity-10"
                              >
                                REVOKE
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )
                  }

                  return (
                    <Fragment key={source.id}>
                      <tr className="border-b border-console-border/70 align-top">
                        <td className="py-2 px-2 font-semibold">{source.id}</td>
                        <td className="py-2 px-2">
                          <input
                            value={source.label}
                            disabled={!canManageThisSource}
                            onFocus={() => setEditingSourceLabelID(source.id)}
                            onBlur={() => setEditingSourceLabelID((prev) => prev === source.id ? null : prev)}
                            onChange={(e) => {
                              const nextLabel = e.target.value
                              setSourcesMap((prev) => ({
                                ...prev,
                                [source.id]: { ...(prev[source.id] ?? source), label: nextLabel },
                              }))
                              setDirtySourceLabelMap((prev) => {
                                if (prev[source.id]) {
                                  return prev
                                }
                                return { ...prev, [source.id]: true }
                              })
                            }}
                            className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                          />
                        </td>
                        <td className="py-2 px-2">
                          <input
                            type="checkbox"
                            checked={source.enabled}
                            disabled={!canManageThisSource}
                            onChange={(e) => setSourcesMap((prev) => ({ ...prev, [source.id]: { ...source, enabled: e.target.checked } }))}
                          />
                        </td>
                        <td className="py-2 px-2">
                          <input
                            type="checkbox"
                            checked={source.isShared}
                            disabled={authUser?.role !== 'admin'}
                            onChange={(e) => toggleSourceShared(source, e.target.checked)}
                            title={source.isShared ? 'Shared with users' : 'Private integration'}
                          />
                        </td>
                        <td className="py-2 px-2">
                          <button
                            onClick={() => toggleSourceKeyPanel(source.id)}
                            className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                            disabled={!canManageThisSource}
                          >
                            {expandedSourceID === source.id ? 'HIDE' : 'SHOW'}
                          </button>
                        </td>
                        <td className="py-2 px-2">
                          <div className="flex items-center gap-2">
                            <button
                              onClick={() => saveSourceSettings(source.id)}
                              className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                              disabled={!canManageThisSource || savingSourceID === source.id || deletingSourceID === source.id}
                            >
                              {savingSourceID === source.id ? 'SAVING...' : 'SAVE'}
                            </button>
                            <button
                              onClick={() => deleteSource(source)}
                              className="px-2 py-1 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                              disabled={!canManageThisSource || deletingSourceID === source.id || savingSourceID === source.id}
                            >
                              {deletingSourceID === source.id ? 'DELETING...' : 'DELETE'}
                            </button>
                          </div>
                        </td>
                      </tr>
                      {expandedSourceID === source.id && (
                        <tr className="border-b border-console-border/70 bg-console-surface/40">
                          <td className="py-3 px-2 text-[11px] text-console-muted" colSpan={6}>
                            <div className="flex flex-col gap-3">
                              <div className="flex items-center justify-between gap-3">
                                <span className="console-label text-[10px]">API KEYS</span>
                                <button
                                  onClick={() => generateKey(source.id)}
                                  className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                                  disabled={!canManageThisSource || generatingKeyFor === source.id}
                                >
                                  {generatingKeyFor === source.id ? 'GENERATING...' : 'GENERATE KEY'}
                                </button>
                              </div>
                              {keyPanel}
                              {authUser?.role === 'admin' && (
                                <div className="border-t border-console-border/70 pt-3 flex flex-col gap-2">
                                  <div className="flex items-center justify-between gap-3">
                                    <span className="console-label text-[10px]">SHARE WITH USERS</span>
                                    <span className="text-[10px] text-console-muted">
                                      {sourceShareStatus}
                                    </span>
                                  </div>
                                  {users.length === 0 ? (
                                    <div className="text-console-muted">No users found</div>
                                  ) : (
                                    <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
                                      {users.filter((user) => user.id !== source.userId).map((user) => {
                                        const checked = (sourceShares[source.id] || []).includes(user.id)
                                        return (
                                          <label key={user.id} className="flex items-center gap-2 border border-console-border/70 rounded px-2 py-1 text-[10px]">
                                            <input
                                              type="checkbox"
                                              checked={checked}
                                              disabled={savingSharesFor === source.id || loadingSharesFor === source.id}
                                              onChange={(e) => updateSourceShareUser(source.id, user.id, e.target.checked)}
                                            />
                                            <span className="truncate" title={`${user.email} (${user.role})`}>
                                              {user.email} [{user.role}]
                                            </span>
                                          </label>
                                        )
                                      })}
                                    </div>
                                  )}
                                </div>
                              )}
                            </div>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>

          <div className="border border-console-border rounded p-3">
            <p className="console-label text-xs mb-2">EXAMPLE REQUEST</p>
            <pre className="text-[11px] text-console-muted whitespace-pre-wrap">{uploadExample}</pre>
          </div>
        </main>
      )}

      {authUser && activeView === 'talkgroups' && (
        <main className="console-panel flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="console-label text-xs">TALKGROUP VIEWER</span>
            <span className="text-xs text-console-muted tabular-nums">{talkgroupRows.length} groups</span>
          </div>
          <input
            value={talkgroupSearch}
            onChange={(e) => setTalkgroupSearch(e.target.value)}
            placeholder="Search talkgroup, label, group, system..."
            className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
          />
          <div className="overflow-auto max-h-[520px]">
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
                  <th className="py-1.5 px-3 text-left font-normal">TG</th>
                  <th className="py-1.5 px-3 text-left font-normal">Label</th>
                  <th className="py-1.5 px-3 text-left font-normal">Group</th>
                  <th className="py-1.5 px-3 text-left font-normal">System</th>
                  <th className="py-1.5 px-3 text-left font-normal">Calls</th>
                  <th className="py-1.5 px-3 text-left font-normal">Last Seen</th>
                  <th className="py-1.5 px-3 text-left font-normal">Flags</th>
                  {authUser.role === 'admin' && <th className="py-1.5 px-3 text-left font-normal">Admin</th>}
                </tr>
              </thead>
              <tbody>
                {talkgroupRows.map((row) => (
                  <tr key={row.talkgroup} className="border-b border-console-border/70">
                    <td className="py-2 px-3 font-semibold text-console-accent">{row.talkgroup}</td>
                    <td className="py-2 px-3">{row.label || <span className="text-console-muted">—</span>}</td>
                    <td className="py-2 px-3 text-console-muted">{row.group || '—'}</td>
                    <td className="py-2 px-3 text-console-muted">{row.system || '—'}</td>
                    <td className="py-2 px-3 tabular-nums">{row.callCount}</td>
                    <td className="py-2 px-3 tabular-nums text-console-muted">
                      {row.lastSeen > 0 ? fmtTime(row.lastSeen) : '—'}
                    </td>
                    <td className="py-2 px-3">
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() =>
                            updateTalkgroupSetting(row.talkgroup, (current) => ({
                              talkgroup: row.talkgroup,
                              favorite: !(current?.favorite ?? row.favorite),
                              muted: current?.muted ?? row.muted,
                              updatedAt: Date.now() / 1000,
                            }))
                          }
                          className={`text-[10px] px-1.5 py-0.5 border rounded ${
                            row.favorite
                              ? 'border-console-accent text-console-accent'
                              : 'border-console-border text-console-muted'
                          }`}
                        >
                          ★
                        </button>
                        <button
                          onClick={() =>
                            updateTalkgroupSetting(row.talkgroup, (current) => ({
                              talkgroup: row.talkgroup,
                              favorite: current?.favorite ?? row.favorite,
                              muted: !(current?.muted ?? row.muted),
                              updatedAt: Date.now() / 1000,
                            }))
                          }
                          className={`text-[10px] px-1.5 py-0.5 border rounded ${
                            row.muted
                              ? 'border-console-error text-console-error'
                              : 'border-console-border text-console-muted'
                          }`}
                        >
                          M
                        </button>
                      </div>
                    </td>
                    {authUser.role === 'admin' && (
                      <td className="py-2 px-3">
                        <button
                          onClick={() => removeTalkgroup(row.talkgroup)}
                          className="text-[10px] px-1.5 py-0.5 border border-console-error text-console-error rounded hover:bg-console-error hover:bg-opacity-10 disabled:opacity-50"
                          disabled={talkgroupActionID === row.talkgroup}
                          title="Remove stored calls for this talkgroup. It will reappear if new calls arrive."
                        >
                          {talkgroupActionID === row.talkgroup ? '...' : 'REMOVE'}
                        </button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </main>
      )}

      {authUser && activeView === 'hub' && (
        <main className="console-panel flex flex-col gap-4">
          <div className="flex items-center justify-between gap-2 flex-wrap">
            <span className="console-label text-xs">HUB IDENTITY</span>
            <span className="text-xs text-console-muted tabular-nums">{hubIdentity?.hubId || 'not initialized'}</span>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <div className="border border-console-border rounded p-3 flex flex-col gap-3">
              <div>
                <p className="console-label text-xs mb-1">DISPLAY NAME</p>
                <input
                  value={hubDraft.name}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, name: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="P7 Scanner Hub"
                  disabled={authUser.role !== 'admin' || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">PUBLIC URL</p>
                <input
                  value={hubDraft.publicUrl}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, publicUrl: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="https://scanner.example.com"
                  disabled={authUser.role !== 'admin' || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">REGION</p>
                <input
                  value={hubDraft.region}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, region: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="Yukon, OK"
                  disabled={authUser.role !== 'admin' || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">CONTACT</p>
                <input
                  value={hubDraft.contact}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, contact: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="info@example.com"
                  disabled={authUser.role !== 'admin' || hubLoading}
                />
              </div>
              <label className="flex items-center gap-2 text-xs text-console-muted">
                <input
                  type="checkbox"
                  checked={hubDraft.federationEnabled}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, federationEnabled: event.target.checked }))}
                  disabled={authUser.role !== 'admin' || hubLoading}
                />
                <span>Federation enabled</span>
              </label>
              {authUser.role === 'admin' && (
                <div className="flex gap-2 flex-wrap">
                  <button
                    onClick={saveHubIdentity}
                    className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
                    disabled={hubLoading}
                  >
                    {hubLoading ? 'SAVING...' : 'SAVE HUB'}
                  </button>
                  <button
                    onClick={refreshHubIdentity}
                    className="w-fit px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent disabled:opacity-50"
                    disabled={hubLoading}
                  >
                    REFRESH
                  </button>
                </div>
              )}
            </div>

            <div className="border border-console-border rounded p-3 flex flex-col gap-2 text-xs">
              <p className="console-label text-xs">FEDERATION STATUS</p>
              <div className="text-console-muted">Hub ID: <span className="text-console-text break-all">{hubIdentity?.hubId || '—'}</span></div>
              <div className="text-console-muted">Directory: <span className="text-console-accent uppercase">{hubIdentity?.directoryValidationStatus || 'unverified'}</span></div>
              <div className="text-console-muted">Public key: <span className="text-console-text break-all">{hubIdentity?.publicKey || 'not generated yet'}</span></div>
              <div className="text-console-muted">Updated: <span className="text-console-text">{hubIdentity?.updatedAt ? fmtDateTime(hubIdentity.updatedAt) : '—'}</span></div>
              {authUser.role !== 'admin' && (
                <p className="text-[11px] text-console-muted mt-2">Admin access is required to change hub identity.</p>
              )}
            </div>
          </div>

          {authUser.role === 'admin' && (
            <div className="border border-console-border rounded p-3 flex flex-col gap-3">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <div>
                  <p className="console-label text-xs">UPDATE STATUS</p>
                  <p className="text-[11px] text-console-muted">Public SignalForge image manifest check.</p>
                </div>
                <button
                  onClick={refreshUpdateCheck}
                  className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent"
                >
                  CHECK NOW
                </button>
              </div>
              <div className="grid gap-2 md:grid-cols-2 text-xs">
                <div className="text-console-muted">Current: <span className="text-console-text">{versionInfo?.deployTag || versionInfo?.commit?.slice(0, 8) || 'unknown'}</span></div>
                <div className="text-console-muted">Latest: <span className={updateStatusClass}>{updateInfo?.latest?.imageTag || updateInfo?.latest?.shortCommit || 'unknown'}</span></div>
                <div className="text-console-muted">Namespace: <span className="text-console-text">{updateInfo?.latest?.imageNamespace || 'unknown'}</span></div>
                <div className="text-console-muted">Status: <span className={updateStatusClass}>{updateStatusLabel}</span></div>
              </div>
              {updateInfo?.updateAvailable && updateInfo.latest && (
                <div className="border border-console-amber rounded p-2 text-[11px] text-console-muted">
                  Set <span className="text-console-text">IMAGE_NAMESPACE={updateInfo.latest.imageNamespace}</span> and <span className="text-console-text">IMAGE_TAG={updateInfo.latest.imageTag}</span>, then redeploy with image pull enabled.
                </div>
              )}
              {updateInfo?.error && <div className="text-[11px] text-console-error">{updateInfo.error}</div>}
            </div>
          )}

          {authUser.role === 'admin' && (
            <div className="border border-console-border rounded p-3 flex flex-col gap-3">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <div>
                  <p className="console-label text-xs">CONNECTED PEERS</p>
                  <p className="text-[11px] text-console-muted">Paste a remote hub URL and invite token to join two hubs.</p>
                </div>
                <button
                  onClick={refreshHubPeers}
                  className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent"
                >
                  REFRESH
                </button>
              </div>

              <div className="grid gap-2 md:grid-cols-[1fr_1.4fr_auto] items-end">
                <div>
                  <p className="console-label text-xs mb-1">REMOTE HUB URL</p>
                  <input
                    value={peerRemoteURL}
                    onChange={(event) => setPeerRemoteURL(event.target.value)}
                    className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
                    placeholder="https://remote-hub.example.com"
                  />
                </div>
                <div>
                  <p className="console-label text-xs mb-1">INVITE TOKEN</p>
                  <input
                    value={peerInviteToken}
                    onChange={(event) => setPeerInviteToken(event.target.value)}
                    className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
                    placeholder="hub_invite_..."
                  />
                </div>
                <button
                  onClick={connectHubPeer}
                  className="px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
                  disabled={hubPeerActionID === 'connect' || !hubIdentity?.federationEnabled || !peerRemoteURL.trim() || !peerInviteToken.trim()}
                  title={hubIdentity?.federationEnabled ? 'Connect peer hub' : 'Enable federation before connecting peers'}
                >
                  {hubPeerActionID === 'connect' ? 'CONNECTING...' : 'CONNECT'}
                </button>
              </div>

              <div className="overflow-auto">
                <table className="w-full border-collapse text-xs">
                  <thead>
                    <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
                      <th className="py-1.5 px-2 text-left font-normal">Hub</th>
                      <th className="py-1.5 px-2 text-left font-normal">Status</th>
                      <th className="py-1.5 px-2 text-left font-normal">Direction</th>
                      <th className="py-1.5 px-2 text-left font-normal">Last Seen</th>
                      <th className="py-1.5 px-2 text-left font-normal">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {hubPeers.map((peer) => {
                      const connectedPeer = peer.status === 'connected'
                      return (
                        <tr key={peer.id} className="border-b border-console-border/70">
                          <td className="py-2 px-2">
                            <div className="text-console-text">{peer.name || peer.hubId}</div>
                            <div className="text-[10px] text-console-muted break-all">{peer.publicUrl || peer.hubId}</div>
                            {peer.region && <div className="text-[10px] text-console-muted">{peer.region}</div>}
                          </td>
                          <td className={`py-2 px-2 uppercase ${connectedPeer ? 'text-console-accent' : 'text-console-muted'}`}>{peer.status}</td>
                          <td className="py-2 px-2 uppercase text-console-muted">{peer.direction}</td>
                          <td className="py-2 px-2 text-console-muted tabular-nums">{peer.lastSeenAt ? fmtDateTime(peer.lastSeenAt) : '—'}</td>
                          <td className="py-2 px-2">
                            <button
                              onClick={() => disableHubPeer(peer.id)}
                              className="text-[10px] px-1.5 py-0.5 border border-console-error text-console-error rounded hover:bg-console-error hover:bg-opacity-10 disabled:opacity-50"
                              disabled={!connectedPeer || hubPeerActionID === peer.id}
                            >
                              {hubPeerActionID === peer.id ? '...' : 'DISABLE'}
                            </button>
                          </td>
                        </tr>
                      )
                    })}
                    {hubPeers.length === 0 && (
                      <tr>
                        <td className="py-3 px-2 text-console-muted" colSpan={5}>No connected peers yet</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {authUser.role === 'admin' && (
            <div className="border border-console-border rounded p-3 flex flex-col gap-3">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <div>
                  <p className="console-label text-xs">PEER INVITES</p>
                  <p className="text-[11px] text-console-muted">Generate a 7-day token for another P7 Scanner hub.</p>
                </div>
                <div className="flex gap-2 flex-wrap">
                  <button
                    onClick={createHubInvite}
                    className="px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
                    disabled={hubInviteActionID === 'new' || !hubIdentity?.federationEnabled}
                    title={hubIdentity?.federationEnabled ? 'Generate peer invite token' : 'Enable federation before creating invites'}
                  >
                    {hubInviteActionID === 'new' ? 'CREATING...' : 'GENERATE INVITE'}
                  </button>
                  <button
                    onClick={refreshHubInvites}
                    className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent"
                  >
                    REFRESH
                  </button>
                </div>
              </div>

              <div className="overflow-auto">
                <table className="w-full border-collapse text-xs">
                  <thead>
                    <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
                      <th className="py-1.5 px-2 text-left font-normal">Token</th>
                      <th className="py-1.5 px-2 text-left font-normal">Status</th>
                      <th className="py-1.5 px-2 text-left font-normal">Expires</th>
                      <th className="py-1.5 px-2 text-left font-normal">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {hubInvites.map((invite) => {
                      const expired = invite.expiresAt > 0 && invite.expiresAt <= nowUnix
                      const revoked = invite.revokedAt > 0
                      const used = invite.usedAt > 0
                      const active = !expired && !revoked && !used
                      let status = 'active'
                      if (revoked) status = 'revoked'
                      else if (used) status = 'used'
                      else if (expired) status = 'expired'
                      return (
                        <tr key={invite.id} className="border-b border-console-border/70">
                          <td className="py-2 px-2 max-w-[320px] break-all text-console-muted">{invite.token}</td>
                          <td className={`py-2 px-2 uppercase ${active ? 'text-console-accent' : 'text-console-muted'}`}>{status}</td>
                          <td className="py-2 px-2 text-console-muted tabular-nums">{fmtDateTime(invite.expiresAt)}</td>
                          <td className="py-2 px-2">
                            <div className="flex gap-2 flex-wrap">
                              <button
                                onClick={() => {
                                  copyToClipboard(invite.token)
                                  setHubMessage('Invite token copied')
                                }}
                                className="text-[10px] px-1.5 py-0.5 border border-console-border text-console-muted rounded hover:border-console-accent hover:text-console-accent"
                              >
                                COPY
                              </button>
                              <button
                                onClick={() => revokeHubInvite(invite.id)}
                                className="text-[10px] px-1.5 py-0.5 border border-console-error text-console-error rounded hover:bg-console-error hover:bg-opacity-10 disabled:opacity-50"
                                disabled={!active || hubInviteActionID === invite.id}
                              >
                                {hubInviteActionID === invite.id ? '...' : 'REVOKE'}
                              </button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                    {hubInvites.length === 0 && (
                      <tr>
                        <td className="py-3 px-2 text-console-muted" colSpan={4}>No invites yet</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {hubMessage && <div className="text-[11px] text-console-accent">{hubMessage}</div>}
          {hubError && <div className="text-[11px] text-console-error">{hubError}</div>}
        </main>
      )}

    {activeView === 'account' && (
    <main className="console-panel flex flex-col gap-4">
      <div className="grid gap-3 md:grid-cols-2">
      <div className="border border-console-border rounded p-3">
        <p className="console-label text-xs mb-2">SESSION</p>
        {!authUser && <p className="text-xs text-console-muted">Not authenticated</p>}
        {authUser && (
        <div className="text-xs flex flex-col gap-2">
          <div className="text-console-muted">Email: <span className="text-console-text">{authUser.email}</span></div>
          <div className="text-console-muted">Role: <span className="text-console-accent uppercase">{authUser.role}</span></div>
          <button
          onClick={logoutSession}
          className="w-fit px-2 py-1 border border-console-error text-console-error rounded text-[11px] hover:bg-console-error hover:bg-opacity-10"
          disabled={authLoading}
          >
          {authLoading ? 'WORKING...' : 'LOGOUT'}
          </button>
        </div>
        )}
      </div>

      {!authUser && (
        <div className="border border-console-border rounded p-3 flex flex-col gap-3">
        <p className="console-label text-xs">STEP 1: REQUEST LOGIN</p>
        <p className="text-[11px] text-console-muted">Enter your email address and request a magic link.</p>
        <input
          value={authEmail}
          onChange={(e) => setAuthEmail(e.target.value)}
          placeholder="your.email@example.com"
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
          disabled={authLoading}
        />
        <button
          onClick={requestMagicLink}
          className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
          disabled={authLoading || !authEmail.trim()}
        >
          {authLoading ? 'WORKING...' : 'REQUEST MAGIC LINK'}
        </button>
        </div>
      )}

      {!authUser && (
        <div className="border border-console-border rounded p-3 flex flex-col gap-3">
        <p className="console-label text-xs">STEP 2: VERIFY TOKEN</p>
        <p className="text-[11px] text-console-muted">Check your email for a link. Copy the token and paste it below.</p>
        <input
          value={authToken}
          onChange={(e) => setAuthToken(e.target.value)}
          placeholder="paste token from email"
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
          disabled={authLoading}
        />
        <button
          onClick={verifyMagicLinkToken}
          className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
          disabled={authLoading || !authToken.trim()}
        >
          {authLoading ? 'WORKING...' : 'VERIFY & LOGIN'}
        </button>
        </div>
      )}
      </div>

      {authMessage && <div className="text-[11px] text-console-accent">{authMessage}</div>}
      {authError && <div className="text-[11px] text-console-error">{authError}</div>}

      {authUser?.role === 'admin' && (
      <div className="border border-console-border rounded p-3 overflow-auto">
        <div className="flex items-center justify-between mb-2">
        <p className="console-label text-xs">USER MANAGEMENT</p>
        <button
          onClick={() => refreshUsers()}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
          disabled={usersLoading}
        >
          {usersLoading ? 'LOADING...' : 'REFRESH'}
        </button>
        </div>
        <table className="w-full border-collapse text-xs">
        <thead>
          <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
          <th className="py-1.5 px-2 text-left font-normal">Email</th>
          <th className="py-1.5 px-2 text-left font-normal">Role</th>
          <th className="py-1.5 px-2 text-left font-normal">Status</th>
          <th className="py-1.5 px-2 text-left font-normal">Created</th>
          <th className="py-1.5 px-2 text-left font-normal">Updated</th>
          <th className="py-1.5 px-2 text-left font-normal">Actions</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
          <tr key={user.id} className="border-b border-console-border/70">
            <td className="py-2 px-2">{user.email}</td>
            <td className="py-2 px-2">
            <select
              value={user.role}
              onChange={(e) => setUsers((prev) => prev.map((row) => row.id === user.id ? { ...row, role: e.target.value as UserRecord['role'] } : row))}
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
            >
              <option value="admin">admin</option>
              <option value="user">user</option>
              <option value="guest">guest</option>
            </select>
            </td>
            <td className="py-2 px-2">
            <select
              value={user.status}
              onChange={(e) => setUsers((prev) => prev.map((row) => row.id === user.id ? { ...row, status: e.target.value as UserRecord['status'] } : row))}
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
            >
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
            </td>
            <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.createdAt)}</td>
            <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.updatedAt)}</td>
            <td className="py-2 px-2">
            <div className="flex items-center gap-2">
              <button
              onClick={() => saveUser(user)}
              className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
              disabled={userActionID === user.id}
              >
              SAVE
              </button>
              <button
              onClick={() => removeUser(user)}
              className="px-2 py-1 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
              disabled={userActionID === user.id}
              >
              DELETE
              </button>
            </div>
            </td>
          </tr>
          ))}
          {users.length === 0 && (
          <tr>
            <td className="py-3 px-2 text-console-muted" colSpan={6}>No users</td>
          </tr>
          )}
        </tbody>
        </table>
      </div>
      )}

    {authUser?.role === 'admin' && (
    <div className="border border-console-border rounded p-3 overflow-auto">
      <div className="flex items-center justify-between mb-2">
      <p className="console-label text-xs">AUDIT LOG</p>
      <button
        onClick={() => refreshAuditLogs()}
        className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
        disabled={auditLoading}
      >
        {auditLoading ? 'LOADING...' : 'REFRESH'}
      </button>
      </div>
      <table className="w-full border-collapse text-xs">
      <thead>
        <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
        <th className="py-1.5 px-2 text-left font-normal">Time</th>
        <th className="py-1.5 px-2 text-left font-normal">Action</th>
        <th className="py-1.5 px-2 text-left font-normal">Target</th>
        <th className="py-1.5 px-2 text-left font-normal">Actor</th>
        </tr>
      </thead>
      <tbody>
        {auditLogs.map((entry) => (
        <tr key={entry.id} className="border-b border-console-border/70">
          <td className="py-2 px-2 text-console-muted">{fmtDateTime(entry.createdAt)}</td>
          <td className="py-2 px-2">{entry.action}</td>
          <td className="py-2 px-2 text-console-muted">{entry.targetType}:{entry.targetId}</td>
          <td className="py-2 px-2 text-console-muted">{entry.userId || 'system'}</td>
        </tr>
        ))}
        {auditLogs.length === 0 && (
        <tr>
          <td className="py-3 px-2 text-console-muted" colSpan={4}>No audit entries</td>
        </tr>
        )}
      </tbody>
      </table>
    </div>
    )}
    </main>
    )}

      <footer className="text-xs text-console-muted border-t border-console-border pt-3 flex flex-col gap-1">
        <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
          <span>projectseven .Co .Ltd © {new Date().getFullYear()} — ALL SYSTEMS OPERATIONAL</span>
          <span className="flex flex-wrap gap-2 text-[10px] uppercase tracking-widest">
            <a className="hover:text-console-accent" href={footerSourceLinks.source} target="_blank" rel="noopener noreferrer">Source</a>
            <span aria-hidden>/</span>
            <a className="hover:text-console-accent" href={footerSourceLinks.license} target="_blank" rel="noopener noreferrer">AGPLv3</a>
            <span aria-hidden>/</span>
            <a className="hover:text-console-accent" href={footerSourceLinks.fairSource} target="_blank" rel="noopener noreferrer">Fair Source</a>
          </span>
        </div>
        <div className="text-[10px] tabular-nums break-all">{footerDeploymentLabel}</div>
      </footer>
    </div>
  )
}

export default App
