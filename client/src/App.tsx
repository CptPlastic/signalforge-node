import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiError,
  api,
  type AuthCapabilities,
  type AuthUser,
  type RadioSetSelectionMode,
  type AuditLogEntry,
  type Call,
  type FederationStatus,
  type HubIdentity,
  type HubInvite,
  type HubPeer,
  type IngestionSource,
  type RadioSet,
  type SourceAPIKey,
  type TalkgroupInfo,
  type TalkgroupSetting,
  type UserRecord,
} from './lib/api'
import { AppHeader } from './components/AppHeader'
import { AppNav } from './components/AppNav'
import { AuthenticatedView } from './components/ActiveView'
import { AccountView } from './components/account/AccountView'
import { HubRegisterPanel } from './components/hub/HubRegisterPanel'
import { DiscordBotPanel } from './components/hub/DiscordBotPanel'
import { IncidentsPanel } from './components/hub/IncidentsPanel'
import { CallRow } from './components/calls/CallRow'
import { DispatcherView } from './components/radio-sets/DispatcherView'
import { RadioSetPTTView } from './components/radio-sets/RadioSetPTTView'
import { RadioSetsView } from './components/radio-sets/RadioSetsView'
import { SignalForgeLogo } from './components/SignalForgeLogo'
import { useOverallStatus } from './hooks/useOverallStatus'
import { useUpdateCheck } from './hooks/useUpdateCheck'
import { buildFilteredCalls, formatCallLogCount, formatSavedCallCount } from './lib/callFilters'
import { playChirp } from './lib/chirp'
import {
  clampVolume,
  DEFAULT_CHIRP_VOLUME,
  getStoredChirpVolume,
  storeChirpVolume,
  volumeToGain,
} from './lib/monitorAudioSettings'
import { fmtDateTime, fmtTime, getErrorMessage } from './lib/format'
import { directoryStatusClass, trustTextClass } from './lib/hubStatus'
import { maybePlayActiveRadioSetCall } from './lib/radioSetPlayback'
import {
  getSourceRuntimeStatus,
  smoothSourceStatusMap,
  type SmoothedSourceStatus,
} from './lib/sourceStatus'
import { WebSocketClient } from './lib/ws'
import { isMobileUserAgent, tryOpenSignalforgeApp } from './lib/mobileAuthHandoff'
import type { AppView } from './types/app'
import { readInitialAppView } from './types/app'

type WsCallEvent = { type: 'call'; call: Call; sourceId?: string }
type WsSourceDeletedEvent = { type: 'source_deleted'; sourceId: string }
type WsHeartbeatEvent = { type: 'heartbeat'; ts: number }
type WsEvent = WsCallEvent | WsSourceDeletedEvent | WsHeartbeatEvent

type HubIdentityDraft = Pick<HubIdentity, 'name' | 'publicUrl' | 'region' | 'contact' | 'federationEnabled'>
type PublicWSProbeStatus = 'idle' | 'checking' | 'ok' | 'failed' | 'missing-token'

type PublicWSProbeState = {
  status: PublicWSProbeStatus
  detail: string
  checkedAt: number | null
}

type PublicWSProbeRowProps = Readonly<{
  probe: PublicWSProbeState
  token?: string
  onProbeChange: (state: PublicWSProbeState) => void
}>

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>
}

const SESSION_WARNING_WINDOW_SEC = 15 * 60
const CALL_PAGE_SIZE_OPTIONS = [25, 50, 100, 250] as const
const DEFAULT_CALL_PAGE_SIZE = 50
const CALL_PAGE_SIZE_STORAGE_KEY = 'p7_call_page_size'
const RADIO_SET_VOLUME_STORAGE_KEY = 'p7_radio_set_volume'

function getStoredCallPageSize(): number {
  const saved = Number(localStorage.getItem(CALL_PAGE_SIZE_STORAGE_KEY))
  return CALL_PAGE_SIZE_OPTIONS.includes(saved as typeof CALL_PAGE_SIZE_OPTIONS[number]) ? saved : DEFAULT_CALL_PAGE_SIZE
}

function getStoredRadioSetVolume(): number {
  const stored = localStorage.getItem(RADIO_SET_VOLUME_STORAGE_KEY)
  if (stored == null) return 100
  const saved = Number(stored)
  if (!Number.isFinite(saved) || saved <= 0) return 100
  return Math.min(100, Math.max(1, Math.round(saved)))
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(console.error)
}

// verifyAuthEntry picks the right verification call: a short numeric value is
// the in-app 6-digit code; anything else is a full magic-link token pasted
// from the email.
function verifyAuthEntry(email: string, entry: string) {
  if (/^\d{4,8}$/.test(entry)) {
    return api.verifyMagicCode(email, entry)
  }
  return api.verifyMagicLink(entry)
}

function redactKey(key: string): string {
  if (key.length <= 8) return key
  return '*'.repeat(key.length - 4) + key.slice(-4)
}

function activeHubInvites(invites: HubInvite[]): HubInvite[] {
  const nowUnix = Math.floor(Date.now() / 1000)
  return invites.filter((invite) => invite.revokedAt === 0 && invite.usedAt === 0 && (!invite.expiresAt || invite.expiresAt > nowUnix))
}

function upsertTalkgroupInfo(talkgroups: TalkgroupInfo[], call: Call): TalkgroupInfo[] {
  if (call.talkgroup <= 0) return talkgroups

  const nextInfo: TalkgroupInfo = {
    talkgroup: call.talkgroup,
    talkgroupLabel: call.talkgroupLabel,
    talkgroupGroup: call.talkgroupGroup,
    systemLabel: call.systemLabel,
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

function publicWSURL(token: string): string {
  const protocol = globalThis.window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${globalThis.window.location.host}/public/ws/${encodeURIComponent(token)}`
}

function probePublicWebSocket(token: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(publicWSURL(token))
    let settled = false
    const timeout = globalThis.window.setTimeout(() => {
      if (settled) return
      settled = true
      ws.close()
      reject(new Error('timeout waiting for public websocket upgrade'))
    }, 5000)

    ws.addEventListener('open', () => {
      if (settled) return
      settled = true
      globalThis.window.clearTimeout(timeout)
      ws.close(1000, 'probe complete')
      resolve()
    })

    ws.addEventListener('error', () => {
      if (settled) return
      settled = true
      globalThis.window.clearTimeout(timeout)
      reject(new Error('public websocket upgrade failed'))
    })

    ws.addEventListener('close', (event) => {
      if (settled) return
      settled = true
      globalThis.window.clearTimeout(timeout)
      reject(new Error(`closed before upgrade (${event.code || 'no-code'})`))
    })
  })
}

function publicWSProbeLabel(status: PublicWSProbeStatus): string {
  const labels: Record<PublicWSProbeStatus, string> = {
    idle: 'NOT CHECKED',
    checking: 'CHECKING',
    ok: 'OK',
    failed: 'FAILED',
    'missing-token': 'NO TOKEN',
  }
  return labels[status]
}

function publicWSProbeClass(status: PublicWSProbeStatus): string {
  if (status === 'ok') return 'text-console-accent'
  if (status === 'failed' || status === 'missing-token') return 'text-console-error'
  return 'text-console-muted'
}

type TranscriptStatusSummary = Readonly<{
  total: number
  done: number
  pending: number
  processing: number
  skipped: number
  failed: number
}>

function summarizeTranscriptStatus(calls: Call[]): TranscriptStatusSummary {
  return calls.reduce<TranscriptStatusSummary>((summary, call) => {
    const status = call.transcriptText ? 'done' : call.transcriptStatus
    if (!status) return summary
    return {
      total: summary.total + 1,
      done: summary.done + (status === 'done' ? 1 : 0),
      pending: summary.pending + (status === 'pending' ? 1 : 0),
      processing: summary.processing + (status === 'processing' ? 1 : 0),
      skipped: summary.skipped + (status === 'skipped' ? 1 : 0),
      failed: summary.failed + (status === 'failed' ? 1 : 0),
    }
  }, { total: 0, done: 0, pending: 0, processing: 0, skipped: 0, failed: 0 })
}

function transcriptSummaryLabel(summary: TranscriptStatusSummary): string {
  if (summary.total === 0) return ''
  const backlog = summary.pending + summary.processing + summary.failed
  if (backlog === 0) return summary.skipped > 0 ? `TX ${summary.done}/${summary.total} · ${summary.skipped} skip` : `TX ${summary.done}/${summary.total}`
  return [
    summary.done > 0 ? `${summary.done} done` : '',
    summary.pending > 0 ? `${summary.pending} queue` : '',
    summary.processing > 0 ? `${summary.processing} run` : '',
    summary.skipped > 0 ? `${summary.skipped} skip` : '',
    summary.failed > 0 ? `${summary.failed} fail` : '',
  ].filter(Boolean).join(' · ')
}

function transcriptSummaryClass(summary: TranscriptStatusSummary): string {
  if (summary.failed > 0) return 'border-console-error text-console-error'
  if (summary.pending + summary.processing > 0) return 'border-console-amber text-console-amber'
  return 'border-console-accent text-console-accent'
}

async function runPublicWSProbe(token: string | undefined, setPublicWSProbe: (state: PublicWSProbeState) => void) {
  if (!token) {
    setPublicWSProbe({ status: 'missing-token', detail: 'no shared radio set token available', checkedAt: Math.floor(Date.now() / 1000) })
    return
  }

  setPublicWSProbe({ status: 'checking', detail: `checking ${publicWSURL(token)}`, checkedAt: null })
  try {
    await probePublicWebSocket(token)
    setPublicWSProbe({ status: 'ok', detail: `upgrade ok: ${publicWSURL(token)}`, checkedAt: Math.floor(Date.now() / 1000) })
  } catch (err) {
    setPublicWSProbe({ status: 'failed', detail: getErrorMessage(err, 'public websocket upgrade failed'), checkedAt: Math.floor(Date.now() / 1000) })
  }
}

function PublicWSProbeRow({
  probe,
  token,
  onProbeChange,
}: PublicWSProbeRowProps) {
  return (
    <>
      <div className="flex items-center gap-2 flex-wrap pt-1">
        <span className="text-console-muted">Public WS:</span>
        <span className={publicWSProbeClass(probe.status)}>
          {publicWSProbeLabel(probe.status)}
        </span>
        <button
          onClick={() => runPublicWSProbe(token, onProbeChange)}
          disabled={probe.status === 'checking'}
          className="px-1.5 py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
        >
          {probe.status === 'checking' ? '...' : 'CHECK'}
        </button>
      </div>
      <div className="text-[10px] text-console-muted break-all">
        {probe.detail}
        {probe.checkedAt && ` · ${fmtTime(probe.checkedAt)}`}
      </div>
    </>
  )
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
  const [activeView, setActiveView] = useState<AppView>(() => readInitialAppView())
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
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null)
  const [isStandalone, setIsStandalone] = useState(() => globalThis.matchMedia('(display-mode: standalone)').matches)
  const [authUser, setAuthUser] = useState<AuthUser | null>(null)
  const [authLoading, setAuthLoading] = useState(false)
  const [authEmail, setAuthEmail] = useState('')
  const [authToken, setAuthToken] = useState('')
  const [authPassword, setAuthPassword] = useState('')
  const [authCapabilities, setAuthCapabilities] = useState<AuthCapabilities | null>(null)
  const [authMessage, setAuthMessage] = useState('')
  const [authError, setAuthError] = useState('')
  const [mobileAppHandoff, setMobileAppHandoff] = useState<{ token: string } | null>(null)
  const [hubIdentity, setHubIdentity] = useState<HubIdentity | null>(null)
  const [hubDraft, setHubDraft] = useState<HubIdentityDraft>({ name: '', publicUrl: '', region: '', contact: '', federationEnabled: false })
  const [hubLoading, setHubLoading] = useState(false)
  const [hubMessage, setHubMessage] = useState('')
  const [hubError, setHubError] = useState('')
  const [hubInvites, setHubInvites] = useState<HubInvite[]>([])
  const [hubInviteActionID, setHubInviteActionID] = useState<string | null>(null)
  const [hubPeers, setHubPeers] = useState<HubPeer[]>([])
  const [federationStatus, setFederationStatus] = useState<FederationStatus | null>(null)
  const [publicWSProbe, setPublicWSProbe] = useState<PublicWSProbeState>({ status: 'idle', detail: 'not checked', checkedAt: null })
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
  const [callPageSize, setCallPageSize] = useState(getStoredCallPageSize)
  const [savedCallIds, setSavedCallIds] = useState<Set<number>>(new Set())
  const [radioSets, setRadioSets] = useState<RadioSet[]>([])
  const [selectedSetID, setSelectedSetID] = useState('')
  const [rsPlayingID, setRsPlayingID] = useState<string | null>(null)
  const [pttSetID, setPTTSetID] = useState<string | null>(null)
  const [dispatcherActive, setDispatcherActive] = useState(false)
  // latestCall is a one-shot "an incoming call just arrived over the WS"
  // signal for live-monitoring views (currently DispatcherView). It updates
  // only on WS call events — historical-rows merges don't touch it.
  const [latestCall, setLatestCall] = useState<Call | null>(null)
  const [audioDevices, setAudioDevices] = useState<MediaDeviceInfo[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState('')
  const [distinctTalkgroups, setDistinctTalkgroups] = useState<TalkgroupInfo[]>([])
  const [rsName, setRsName] = useState('')
  const [rsCreateTGs, setRsCreateTGs] = useState<number[]>([])
  const [rsCreateMode, setRsCreateMode] = useState<RadioSetSelectionMode>('talkgroups')
  const [rsCreateGroups, setRsCreateGroups] = useState<string[]>([])
  const [rsTGSearch, setRsTGSearch] = useState('')
  const [rsGroupSearch, setRsGroupSearch] = useState('')
  const [rsEditID, setRsEditID] = useState<string | null>(null)
  const [rsEditName, setRsEditName] = useState('')
  const [rsEditTGs, setRsEditTGs] = useState<number[]>([])
  const [rsEditMode, setRsEditMode] = useState<RadioSetSelectionMode>('talkgroups')
  const [rsEditGroups, setRsEditGroups] = useState<string[]>([])
  const [rsError, setRsError] = useState('')
  const [rsLoading, setRsLoading] = useState(false)
  const [rsVolume, setRsVolume] = useState(getStoredRadioSetVolume)
  const [chirpVolume, setChirpVolumeState] = useState(getStoredChirpVolume)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const rsVolumeRef = useRef(rsVolume)
  const chirpVolumeRef = useRef(chirpVolume)

  const setChirpVolume = useCallback((value: number) => {
    setChirpVolumeState(clampVolume(value, DEFAULT_CHIRP_VOLUME))
  }, [])

  const {
    versionInfo,
    updateInfo,
    refreshUpdateCheck,
    headerVersionLabel,
    headerVersionTitle,
    footerDeploymentLabel,
    currentDeploymentTag,
    latestUpdateTag,
    hasUpdateAvailable,
    updateTitle,
    updateStatusLabel,
    updateStatusClass,
  } = useUpdateCheck()
  const transcriptionEnabled = versionInfo?.transcriptionEnabled !== false
  const isAdmin = authUser?.role === 'admin'

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

  const finalizeVerifiedSession = async (email: string, role: AuthUser['role']) => {
    setActiveView('account')
    await refreshAuthSession()
    setAuthMessage(`Signed in as ${email}`)
    if (activeView === 'account' && role === 'admin') {
      await refreshUsers()
    }
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
  const byID = new Map(existing.map((call) => [call.id, call]))
  for (const call of incoming) {
    byID.set(call.id, { ...byID.get(call.id), ...call })
  }
  const merged = Array.from(byID.values())
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
      .then((invites) => setHubInvites(activeHubInvites(invites)))
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

  const refreshFederationStatus = () => {
    if (authUser?.role !== 'admin') {
      setFederationStatus(null)
      return Promise.resolve()
    }
    return api.federationStatus()
      .then(setFederationStatus)
      .catch((err) => {
        console.error(err)
        setHubError(getErrorMessage(err, 'Could not load federation status'))
      })
  }

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
    void (async () => {
      try {
        setAuthCapabilities(await api.authCapabilities())
        return
      } catch {
        // Fall back to /version when an older proxy blocks capabilities (rare).
      }
      try {
        const version = await api.version()
        setAuthCapabilities({
          passwordLoginEnabled: Boolean(version.passwordLoginEnabled),
          magicLinkEnabled: true,
          emailDeliveryConfigured: Boolean(version.emailDeliveryConfigured),
          autoApproveUsers: false,
        })
      } catch {
        // Account view will show magic-link only.
      }
    })()
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

  refreshAuthSession()
  }, [])

  useEffect(() => {
    if (!authUser) {
      setRadioSets([])
      setDistinctTalkgroups([])
      setHubIdentity(null)
      setHubInvites([])
      setHubPeers([])
      setFederationStatus(null)
      return
    }
    api.radioSets().then(setRadioSets).catch(console.error)
    refreshDistinctTalkgroups()
    refreshHubIdentity()
    refreshHubInvites()
    refreshHubPeers()
    refreshFederationStatus()
  }, [authUser])

  useEffect(() => {
  const params = new URLSearchParams(globalThis.window.location.search)
  if (!params.has('view')) return
  params.delete('view')
  const qs = params.toString()
  globalThis.window.history.replaceState({}, '', qs ? `?${qs}` : globalThis.window.location.pathname)
  }, [])

  useEffect(() => {
  const token = new URLSearchParams(globalThis.window.location.search).get('token')
  if (!token) return

  globalThis.window.history.replaceState({}, '', globalThis.window.location.pathname)

  if (isMobileUserAgent()) {
    setMobileAppHandoff({ token })
    tryOpenSignalforgeApp(globalThis.window.location.origin, token)
    return
  }

  setAuthToken(token)
  setAuthLoading(true)
  setAuthMessage('')
  setAuthError('')
  api.verifyMagicLink(token)
    .then(async (result) => {
    setAuthUser(result.user)
    await finalizeVerifiedSession(result.user.email, result.user.role)
    })
    .catch((err) => {
    setAuthError(getErrorMessage(err, 'Magic-link verification failed'))
    })
    .finally(() => {
    setAuthLoading(false)
    })
  }, [])

  const continueMagicLinkInBrowser = () => {
    if (!mobileAppHandoff) return
    const token = mobileAppHandoff.token
    setMobileAppHandoff(null)
    setAuthToken(token)
    setAuthLoading(true)
    setAuthMessage('')
    setAuthError('')
    api.verifyMagicLink(token)
      .then(async (result) => {
        setAuthUser(result.user)
        await finalizeVerifiedSession(result.user.email, result.user.role)
      })
      .catch((err) => {
        setAuthError(getErrorMessage(err, 'Magic-link verification failed'))
      })
      .finally(() => {
        setAuthLoading(false)
      })
  }

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

  const handleRefreshSession = useCallback(async () => {
    setAuthError('')
    try {
      const { sessionExpiresAt: newExpiresAt } = await api.refreshSession()
      setSessionExpiresAt(newExpiresAt || null)
      setSessionWarning('')
    } catch (err) {
      setAuthError(getErrorMessage(err, 'Could not refresh session'))
    }
  }, [])

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
    setSourceStatusMap((prev) => smoothSourceStatusMap(prev, sourcesMap, nowUnix))
  }, [nowUnix, sourcesMap])

  useEffect(() => {
    rsVolumeRef.current = rsVolume
    localStorage.setItem(RADIO_SET_VOLUME_STORAGE_KEY, String(rsVolume))
    if (audioRef.current) {
      audioRef.current.volume = rsVolume / 100
    }
  }, [rsVolume])

  useEffect(() => {
    chirpVolumeRef.current = chirpVolume
    storeChirpVolume(chirpVolume)
  }, [chirpVolume])


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
            setLatestCall(msg.call)
            setDistinctTalkgroups((prev) => upsertTalkgroupInfo(prev, msg.call))
            if (msg.call.talkgroupGroup) {
              setAllGroups((prev) => appendSortedGroup(prev, msg.call.talkgroupGroup))
            }

            maybePlayActiveRadioSetCall({
              call: msg.call,
              playCall,
              setPlayingId,
              setRadioSets,
              setRsPlayingID,
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
    audio.src = `/api/v1/calls/${call.id}/audio?play=1`
    const audioVolume = volumeToGain(rsVolumeRef.current)
    audio.volume = audioVolume
    audio.onended = () => setPlayingId(null)
    if (selectedDeviceId && 'setSinkId' in audio) {
      (audio as HTMLAudioElement & { setSinkId(id: string): Promise<void> })
        .setSinkId(selectedDeviceId)
        .catch(console.error)
    }
    audioRef.current = audio
    const chirpReady = call.origin === 'ptt'
      ? playChirp(volumeToGain(chirpVolumeRef.current))
      : Promise.resolve()
    chirpReady.then(() => audio.play())
      .then(() => setPlayingId(call.id))
      .catch((err) => {
        console.error(err)
        setPlayingId(null)
      })
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
        transcribe: next.transcribe,
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
      refreshFederationStatus()
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
      refreshFederationStatus()
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
      setSourceKeys((prev) => ({ ...prev, [sourceId]: keys.filter((key) => !key.revokedAt) }))
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
      await loadSourceKeys(sourceId)
    } catch (err) {
      console.error(err)
    }
  }

  async function toggleSourceKeyPanel(sourceId: string) {
    if (!requireSourceWriteAccess('Sign in to manage source keys')) {
      return
    }
    const isOpen = expandedSourceID === sourceId
    setExpandedSourceID(isOpen ? null : sourceId)
    if (!isOpen) {
      await loadSourceKeys(sourceId)
    }
    if (authUser?.role === 'admin' && !isOpen && !sourceShares[sourceId]) {
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

  async function passwordLogin() {
    const email = authEmail.trim()
    const password = authPassword
    if (!email) {
      setAuthError('Email is required')
      return
    }
    if (!password) {
      setAuthError('Password is required')
      return
    }

    setAuthLoading(true)
    setAuthError('')
    setAuthMessage('')
    try {
      const result = await api.passwordLogin(email, password)
      setAuthUser(result.user)
      setAuthPassword('')
      await finalizeVerifiedSession(result.user.email, result.user.role)
    } catch (err) {
      setAuthError(getErrorMessage(err, 'Sign-in failed'))
    } finally {
      setAuthLoading(false)
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
    if (result.status === 'pending') {
      setAuthMessage(`Access requested for ${email}. An admin must approve this account before login.`)
      return
    }
    setAuthMessage(`Code sent to ${email}. Enter the 6-digit code below, or tap the link in the email.`)
    if (result.code) {
      setAuthToken(result.code)
    } else if (result.token) {
      setAuthToken(result.token)
    }
  } catch (err) {
    setAuthError(getErrorMessage(err, 'Could not request magic link'))
  } finally {
    setAuthLoading(false)
  }
  }

  async function verifyMagicLinkToken() {
  const entry = authToken.trim()
  if (!entry) {
    setAuthError('Enter the code from your email')
    return
  }

  setAuthLoading(true)
  setAuthError('')
  setAuthMessage('')
  try {
    // A short numeric entry is the in-app 6-digit code; anything else is a
    // full magic-link token pasted from the email.
    const result = await verifyAuthEntry(authEmail.trim(), entry)
    setAuthUser(result.user)
    await finalizeVerifiedSession(result.user.email, result.user.role)
  } catch (err) {
    setAuthError(getErrorMessage(err, 'Sign-in verification failed'))
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
    await api.updateUser(user.id, {
      role: user.role,
      status: user.status,
      txEnabled: user.txEnabled,
      dispatcherEnabled: user.dispatcherEnabled,
    })
    await refreshUsers()
  } catch (err) {
    setAuthError(getErrorMessage(err, `Could not update ${user.email}`))
  } finally {
    setUserActionID(null)
  }
  }

  async function approveUser(user: UserRecord) {
  setUserActionID(user.id)
  setAuthError('')
  setAuthMessage('')
  try {
    await api.updateUser(user.id, { role: user.role, status: 'active' })
    await refreshUsers()
    setAuthMessage(`Approved ${user.email}`)
  } catch (err) {
    setAuthError(getErrorMessage(err, `Could not approve ${user.email}`))
  } finally {
    setUserActionID(null)
  }
  }

  async function setUserPassword(user: UserRecord, password: string) {
  setUserActionID(user.id)
  setAuthError('')
  setAuthMessage('')
  try {
    await api.setUserPassword(user.id, password)
    await refreshUsers()
    setAuthMessage(`Password updated for ${user.email}`)
  } catch (err) {
    setAuthError(getErrorMessage(err, `Could not set password for ${user.email}`))
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

  const filteredCalls = useMemo(() => buildFilteredCalls({
    calls,
    groupFilter,
    hideMuted,
    search,
    serverResults,
    settingsMap,
    showFavoritesOnly,
    sortBy,
    sortOrder,
  }), [calls, serverResults, hideMuted, search, groupFilter, settingsMap, showFavoritesOnly, sortBy, sortOrder])
  const callLogCountLabel = formatCallLogCount(serverLoading, serverResults, filteredCalls.length, calls.length)
  const transcriptStatusSummary = useMemo(
    () => (transcriptionEnabled ? summarizeTranscriptStatus(filteredCalls) : { total: 0, done: 0, pending: 0, processing: 0, skipped: 0, failed: 0 }),
    [filteredCalls, transcriptionEnabled],
  )
  const transcriptStatusLabel = transcriptionEnabled ? transcriptSummaryLabel(transcriptStatusSummary) : ''

  useEffect(() => {
    setCallPage(0)
  }, [search, groupFilter, sortBy, sortOrder, showFavoritesOnly, hideMuted, selectedSetID, callPageSize])

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

    const selectedSet = selectedSetID ? radioSets.find((rs) => rs.id === selectedSetID) : undefined
    const setTalkgroups = selectedSet && selectedSet.selectionMode !== 'groups'
      ? (selectedSet.pttTalkgroup !== undefined
          ? [...selectedSet.talkgroups, selectedSet.pttTalkgroup]
          : selectedSet.talkgroups)
      : []
    const setGroups = selectedSet?.selectionMode === 'groups' ? (selectedSet.talkgroupGroups ?? []) : []

    if (!q && !groupFilter && favTalkgroups.length === 0 && setTalkgroups.length === 0 && setGroups.length === 0) {
      setServerResults(null)
      setServerLoading(false)
      return
    }
    // If favorites/group/set only (no text search), fire immediately; otherwise debounce
    const delay = q ? 300 : 0
    setServerLoading(true)
    const timer = setTimeout(() => {
      const params: { limit: number; sort: 'datetime'; order: 'desc'; q?: string; group?: string; groups?: string[]; talkgroups?: number[] } = { limit: 1000, sort: 'datetime', order: 'desc' }
      if (q) params.q = q
      if (groupFilter) params.group = groupFilter
      if (setGroups.length > 0) {
        params.groups = setGroups
      } else if (setTalkgroups.length > 0) {
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
    () => filteredCalls.slice(callPage * callPageSize, (callPage + 1) * callPageSize),
    [filteredCalls, callPage, callPageSize],
  )
  const totalCallPages = Math.max(1, Math.ceil(filteredCalls.length / callPageSize))
  const awaitingMagicLink = !authUser && Boolean(authMessage) && !authError

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

  async function refreshHubDirectory() {
    if (authUser?.role !== 'admin') return
    setHubLoading(true)
    setHubMessage('')
    setHubError('')
    try {
      const saved = await api.refreshHubDirectory()
      applyHubIdentity(saved)
      setHubMessage('Directory trust refreshed')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not refresh directory trust'))
    } finally {
      setHubLoading(false)
    }
  }

  async function generateHubKeyPair() {
    if (authUser?.role !== 'admin') return
    setHubLoading(true)
    setHubMessage('')
    setHubError('')
    try {
      const saved = await api.generateHubKeyPair()
      applyHubIdentity(saved)
      setHubMessage('Hub keypair generated')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not generate hub keypair'))
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
      setHubInvites((prev) => activeHubInvites(prev.map((row) => row.id === id ? invite : row)))
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
      refreshFederationStatus()
      setHubMessage('Peer hub connected')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not connect peer hub'))
    } finally {
      setHubPeerActionID(null)
    }
  }

  async function deleteHubPeer(id: string) {
    if (authUser?.role !== 'admin') return
    setHubPeerActionID(id)
    setHubMessage('')
    setHubError('')
    try {
    await api.deleteHubPeer(id)
    setHubPeers((prev) => prev.filter((row) => row.id !== id))
      refreshFederationStatus()
    setHubMessage('Peer removed')
    } catch (err) {
      console.error(err)
    setHubError(getErrorMessage(err, 'Could not remove peer'))
    } finally {
      setHubPeerActionID(null)
    }
  }

  async function enableHubPeer(id: string) {
    if (authUser?.role !== 'admin') return
    setHubPeerActionID(id)
    setHubMessage('')
    setHubError('')
    try {
      const peer = await api.enableHubPeer(id)
      setHubPeers((prev) => prev.map((row) => row.id === id ? peer : row))
      refreshFederationStatus()
      setHubMessage('Peer enabled')
    } catch (err) {
      console.error(err)
      setHubError(getErrorMessage(err, 'Could not enable peer'))
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
      a.remove()
      await new Promise<void>((resolve) => globalThis.setTimeout(resolve, 500))
    }
  }

  function exportCSV() {
    const headers = ['ID', 'DateTime', 'System', 'SystemLabel', 'Talkgroup', 'TalkgroupLabel', 'TalkgroupGroup', 'TalkgroupTag', 'Frequency(Hz)', 'Duration(s)', 'Transcript']
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
      c.transcriptText ?? '',
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
    a.remove()
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
      transcribe: boolean
    }>()

    distinctTalkgroups.forEach((tg) => {
      const setting = settingsMap[tg.talkgroup]
      rows.set(tg.talkgroup, {
        talkgroup: tg.talkgroup,
        label: tg.talkgroupLabel,
        group: tg.talkgroupGroup,
        system: tg.systemLabel,
        callCount: 0,
        lastSeen: 0,
        favorite: setting?.favorite ?? false,
        muted: setting?.muted ?? false,
        transcribe: setting?.transcribe ?? false,
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
          transcribe: setting?.transcribe ?? false,
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
        transcribe: setting?.transcribe ?? current.transcribe,
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
          transcribe: setting.transcribe,
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
        transcribe: setting.transcribe,
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

  const publicWSProbeSet = useMemo(
    () => radioSets.find((set) => Boolean(set.shareToken)),
    [radioSets],
  )

  const overallStatus = useOverallStatus({
    apiHealthy,
    apiLastHealthyUnix,
    authUser,
    connected,
    nowUnix,
    sourceStatusMap,
    sourcesMap,
    wsLastDisconnectUnix,
  })

  return (
    <div className="min-h-screen bg-console-bg text-console-text font-mono px-3 py-3 sm:px-6 sm:py-5 flex flex-col gap-3 sm:gap-4 overflow-x-hidden">
      {mobileAppHandoff && !authUser ? (
        <div className="fixed inset-0 z-[100] flex flex-col items-center justify-center gap-5 bg-console-bg/95 p-6 text-center">
          <SignalForgeLogo className="h-10 w-auto opacity-90" />
          <p className="text-console-accent text-sm uppercase tracking-wider">Opening SignalForge app…</p>
          <p className="text-console-muted text-xs max-w-sm leading-relaxed">
            Tap the link in your email to sign in. If the app did not open, use the button below to sign in here instead.
          </p>
          <button
            type="button"
            onClick={continueMagicLinkInBrowser}
            className="px-4 py-2 border border-console-border text-console-muted rounded text-xs uppercase tracking-wider hover:border-console-accent hover:text-console-accent"
          >
            Continue in browser
          </button>
        </div>
      ) : null}
      <AppHeader
        authUser={authUser}
        headerVersionLabel={headerVersionLabel}
        headerVersionTitle={headerVersionTitle}
        hasUpdateAvailable={hasUpdateAvailable}
        hubIdentity={hubIdentity}
        isStandalone={isStandalone}
        onInstallApp={installApp}
        onShowHub={() => setActiveView('hub')}
        overallStatus={overallStatus}
        showInstallButton={Boolean(installPrompt && !isStandalone)}
        updateError={updateInfo?.error}
        updateTitle={updateTitle}
      />

      <AppNav
        activeView={activeView}
        authUser={authUser}
        onViewChange={setActiveView}
        showIncidentsTab={isAdmin || !!authUser?.dispatcherEnabled}
      />

    {sessionWarning && (
    <div className="console-panel border border-console-error text-console-error text-xs px-3 py-2">
      <div className="flex items-center justify-between gap-3">
      <span>{sessionWarning}</span>
      <button
        onClick={handleRefreshSession}
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
        <SignalForgeLogo className="mx-auto h-24 w-24 text-white drop-shadow-[0_0_16px_rgba(255,170,0,0.2)]" />
        <div className="space-y-2">
          <p className="text-sm font-bold tracking-widest">[ SIGNALFORGE HUB ]</p>
          <p className="text-console-muted text-[11px] leading-6">
            Private console for monitoring, organizing, and sharing community radio traffic through SignalForge.
          </p>
          <p className="text-console-muted text-[11px] leading-6">
            Sign in or create an account to manage sources, radio sets, talkgroups, and live call playback. Recorder downloads are published publicly through SignalForge.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row sm:justify-center">
          <button
            onClick={() => setActiveView('account')}
            className="px-3 py-2 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10"
          >
            CREATE ACCOUNT / SIGN IN
          </button>
          <a
            href="https://signalforge.org/join.html"
            target="_blank"
            rel="noopener noreferrer"
            className="px-3 py-2 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-widest hover:border-console-accent hover:text-console-accent"
          >
            JOIN GUIDE ↗
          </a>
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

      <AuthenticatedView activeView={activeView} authUser={authUser} view="monitor">
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
              <div className="flex items-center gap-2 text-xs text-console-muted tabular-nums">
                {transcriptStatusLabel && (
                  <span className={`px-1.5 py-0.5 border rounded text-[10px] uppercase tracking-widest ${transcriptSummaryClass(transcriptStatusSummary)}`} title="Transcription status for current call list">
                    {transcriptStatusLabel}
                  </span>
                )}
                <span>
                  {callLogCountLabel}
                  {totalCallPages > 1 && (
                    <span className="ml-2 text-[10px]">· pg {callPage + 1}/{totalCallPages}</span>
                  )}
                </span>
              </div>
            </div>

            <div className="mb-3 grid gap-2 md:grid-cols-[2fr_1fr_1fr_1fr_1fr_1fr_auto_auto_auto]">
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
              <select
                value={callPageSize}
                onChange={(e) => {
                  const nextSize = Number(e.target.value)
                  setCallPageSize(nextSize)
                  localStorage.setItem(CALL_PAGE_SIZE_STORAGE_KEY, String(nextSize))
                }}
                className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                title="Calls shown per page"
              >
                {CALL_PAGE_SIZE_OPTIONS.map((size) => (
                  <option key={size} value={size}>Show: {size}</option>
                ))}
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
                  {formatSavedCallCount(savedCallIds.size)}
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
                      <th className="py-1.5 px-3 text-left font-normal">Transcript</th>
                      <th className="py-1.5 px-3 text-left font-normal">Audio</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedCalls.map((call) => (
                      <CallRow
                        key={call.id}
                        call={call}
                        active={call.dateTime + Math.max(Math.ceil(call.duration), 8) >= nowUnix}
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
                            transcribe: current?.transcribe ?? false,
                            updatedAt: Date.now() / 1000,
                          }))
                        }
                        onToggleMuted={() =>
                          updateTalkgroupSetting(call.talkgroup, (current) => ({
                            talkgroup: call.talkgroup,
                            favorite: current?.favorite ?? false,
                            muted: !(current?.muted ?? false),
                            transcribe: current?.transcribe ?? false,
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
                  {callPage * callPageSize + 1}–{Math.min((callPage + 1) * callPageSize, filteredCalls.length)} of {filteredCalls.length}
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
      </AuthenticatedView>

      <AuthenticatedView activeView={activeView} authUser={authUser} view="radio-sets">
        {(() => {
          if (dispatcherActive) {
            return (
              <DispatcherView
                radioSets={radioSets}
                latestCall={latestCall}
                rsVolume={rsVolume}
                setRsVolume={setRsVolume}
                chirpVolume={chirpVolume}
                setChirpVolume={setChirpVolume}
                onBack={() => setDispatcherActive(false)}
              />
            )
          }
          const pttSet = pttSetID ? radioSets.find((rs) => rs.id === pttSetID) : null
          if (pttSet) {
            return <RadioSetPTTView radioSet={pttSet} onBack={() => setPTTSetID(null)} />
          }
          return (
            <RadioSetsView
              authUser={authUser}
              allGroups={allGroups}
              audioDevices={audioDevices}
              audioRef={audioRef}
              distinctTalkgroups={distinctTalkgroups}
              enumerateAudioDevices={enumerateAudioDevices}
              radioSets={radioSets}
              rsCreateTGs={rsCreateTGs}
              rsCreateMode={rsCreateMode}
              rsCreateGroups={rsCreateGroups}
              rsEditID={rsEditID}
              rsEditName={rsEditName}
              rsEditTGs={rsEditTGs}
              rsEditMode={rsEditMode}
              rsEditGroups={rsEditGroups}
              rsError={rsError}
              rsLoading={rsLoading}
              rsName={rsName}
              rsPlayingID={rsPlayingID}
              rsTGSearch={rsTGSearch}
              rsGroupSearch={rsGroupSearch}
              rsVolume={rsVolume}
              selectedDeviceId={selectedDeviceId}
              selectedSetID={selectedSetID}
              setRadioSets={setRadioSets}
              setRsCreateTGs={setRsCreateTGs}
              setRsCreateMode={setRsCreateMode}
              setRsCreateGroups={setRsCreateGroups}
              setRsEditID={setRsEditID}
              setRsEditName={setRsEditName}
              setRsEditTGs={setRsEditTGs}
              setRsEditMode={setRsEditMode}
              setRsEditGroups={setRsEditGroups}
              setRsError={setRsError}
              setRsLoading={setRsLoading}
              setRsName={setRsName}
              setRsPlayingID={setRsPlayingID}
              setRsTGSearch={setRsTGSearch}
              setRsGroupSearch={setRsGroupSearch}
              setRsVolume={setRsVolume}
              setSelectedDeviceId={setSelectedDeviceId}
              setSelectedSetID={setSelectedSetID}
              onOpenPTTMode={setPTTSetID}
              onOpenDispatcher={() => setDispatcherActive(true)}
              onNotify={setHubMessage}
            />
          )
        })()}
      </AuthenticatedView>

      <AuthenticatedView activeView={activeView} authUser={authUser} view="integrations">
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
                  <th className="py-1.5 px-2 text-left font-normal">Federation</th>
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
                  let keyPanel

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
                            title={source.isShared ? 'Exported to connected hubs' : 'Not exported to connected hubs'}
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
      </AuthenticatedView>

      <AuthenticatedView activeView={activeView} authUser={authUser} view="talkgroups">
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
                  {isAdmin && <th className="py-1.5 px-3 text-left font-normal">Admin</th>}
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
                              transcribe: current?.transcribe ?? row.transcribe,
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
                              transcribe: current?.transcribe ?? row.transcribe,
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
                        <button
                          onClick={() =>
                            updateTalkgroupSetting(row.talkgroup, (current) => ({
                              talkgroup: row.talkgroup,
                              favorite: current?.favorite ?? row.favorite,
                              muted: current?.muted ?? row.muted,
                              transcribe: !(current?.transcribe ?? row.transcribe),
                              updatedAt: Date.now() / 1000,
                            }))
                          }
                          className={`text-[10px] px-1.5 py-0.5 border rounded ${
                            row.transcribe
                              ? 'border-console-accent text-console-accent'
                              : 'border-console-border text-console-muted'
                          }`}
                          title={row.transcribe ? 'Transcription enabled for this talkgroup' : 'Enable transcription for this talkgroup'}
                        >
                          TX
                        </button>
                      </div>
                    </td>
                    {isAdmin && (
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
      </AuthenticatedView>

      <AuthenticatedView activeView={activeView} authUser={authUser} view="incidents">
        {(isAdmin || authUser?.dispatcherEnabled) && authUser ? (
          <main className="console-panel">
            <IncidentsPanel
              authUser={authUser}
              isAdmin={isAdmin}
              hubPeers={hubPeers}
              onNotify={setHubMessage}
              onOpenRadioSet={(radioSetId) => {
                setSelectedSetID(radioSetId)
                setRsPlayingID(radioSetId)
                setActiveView('radio-sets')
              }}
            />
          </main>
        ) : (
          <main className="console-panel text-xs text-console-muted">
            Incident management requires admin or dispatcher role.
          </main>
        )}
      </AuthenticatedView>

      <AuthenticatedView activeView={activeView} authUser={authUser} view="hub">
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
                  placeholder="SignalForge Hub"
                  disabled={!isAdmin || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">PUBLIC URL</p>
                <input
                  value={hubDraft.publicUrl}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, publicUrl: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="https://scanner.example.com"
                  disabled={!isAdmin || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">REGION</p>
                <input
                  value={hubDraft.region}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, region: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="Yukon, OK"
                  disabled={!isAdmin || hubLoading}
                />
              </div>
              <div>
                <p className="console-label text-xs mb-1">CONTACT</p>
                <input
                  value={hubDraft.contact}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, contact: event.target.value }))}
                  className="w-full bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent disabled:opacity-60"
                  placeholder="info@example.com"
                  disabled={!isAdmin || hubLoading}
                />
              </div>
              <label className="flex items-center gap-2 text-xs text-console-muted">
                <input
                  type="checkbox"
                  checked={hubDraft.federationEnabled}
                  onChange={(event) => setHubDraft((prev) => ({ ...prev, federationEnabled: event.target.checked }))}
                  disabled={!isAdmin || hubLoading}
                />
                <span>Federation enabled</span>
              </label>
              {isAdmin && (
                <div className="flex gap-2 flex-wrap">
                  <button
                    onClick={saveHubIdentity}
                    className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
                    disabled={hubLoading}
                  >
                    {hubLoading ? 'SAVING...' : 'SAVE HUB'}
                  </button>
                  <button
                    onClick={() => {
                      refreshHubIdentity()
                      refreshFederationStatus()
                    }}
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
              <div className="text-console-muted">Directory: <span className={`${directoryStatusClass(hubIdentity?.directoryValidationStatus)} uppercase`}>{hubIdentity?.directoryValidationStatus || 'unverified'}</span></div>
              <div className="text-console-muted">Trust: <span className={`${trustTextClass(hubIdentity?.trustLevel)} uppercase`}>{hubIdentity?.trustLevel || 'community'}</span></div>
              <div className="text-console-muted">Trust issuer: <span className="text-console-text break-all">{hubIdentity?.trustIssuerHubId || '—'}</span></div>
              <div className="text-console-muted">Trust verified: <span className="text-console-text">{hubIdentity?.trustVerifiedAt ? fmtDateTime(hubIdentity.trustVerifiedAt) : '—'}</span></div>
              {isAdmin && (
                <>
                  <div className="text-console-muted">Pull peers: <span className={federationStatus?.pullPeerCount ? 'text-console-accent' : 'text-console-error'}>{federationStatus?.pullPeerCount ?? '—'}</span></div>
                  <div className="text-console-muted">Local shared sources: <span className={federationStatus?.sharedSources.length ? 'text-console-accent' : 'text-console-error'}>{federationStatus?.sharedSources.length ?? '—'}</span></div>
                  <div className="text-console-muted">Local exportable calls: <span className={federationStatus?.exportableCallCount ? 'text-console-accent' : 'text-console-error'}>{federationStatus?.exportableCallCount ?? '—'}</span></div>
                  <div className="text-console-muted">Imported remote sources: <span className={federationStatus?.importedSourceCount ? 'text-console-accent' : 'text-console-muted'}>{federationStatus?.importedSourceCount ?? '—'}</span></div>
                  <div className="text-console-muted">Imported remote calls: <span className={federationStatus?.importedCallCount ? 'text-console-accent' : 'text-console-muted'}>{federationStatus?.importedCallCount ?? '—'}</span></div>
                  <button
                    onClick={refreshHubDirectory}
                    disabled={hubLoading}
                    className="w-fit px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
                  >
                    CHECK DIRECTORY
                  </button>
                  <PublicWSProbeRow probe={publicWSProbe} token={publicWSProbeSet?.shareToken} onProbeChange={setPublicWSProbe} />
                  {(federationStatus?.peerStatuses.length || 0) > 0 && (
                    <div className="flex flex-col gap-1 pt-1">
                      {federationStatus?.peerStatuses.map((peerStatus) => (
                        <div key={peerStatus.peerId} className="text-[11px] text-console-muted">
                          <span className="text-console-text">{peerStatus.name || peerStatus.hubId}</span>
                          {' '}remote shared: <span className={peerStatus.remoteSharedSources ? 'text-console-accent' : 'text-console-error'}>{peerStatus.remoteSharedSources}</span>
                          {' '}sample calls: <span className={peerStatus.remoteSampleCalls ? 'text-console-accent' : 'text-console-muted'}>{peerStatus.remoteSampleCalls}</span>
                          {peerStatus.error && <span className="text-console-error"> {peerStatus.error}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                  {(federationStatus?.warnings.length || 0) > 0 && (
                    <div className="flex flex-col gap-1 pt-1">
                      {federationStatus?.warnings.map((warning) => (
                        <div key={warning} className="text-console-error">{warning}</div>
                      ))}
                    </div>
                  )}
                </>
              )}
              <div className="text-console-muted">Public key: <span className="text-console-text break-all">{hubIdentity?.publicKey || 'not generated yet'}</span></div>
              {isAdmin && !hubIdentity?.publicKey && (
                <button
                  onClick={generateHubKeyPair}
                  disabled={hubLoading}
                  className="w-fit px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
                >
                  GENERATE KEY
                </button>
              )}
              <div className="text-console-muted">Updated: <span className="text-console-text">{hubIdentity?.updatedAt ? fmtDateTime(hubIdentity.updatedAt) : '—'}</span></div>
              {!isAdmin && (
                <p className="text-[11px] text-console-muted mt-2">Admin access is required to change hub identity.</p>
              )}
            </div>
          </div>

          {isAdmin && (
            <HubRegisterPanel
              hubIdentity={hubIdentity}
              hubDraft={hubDraft}
              hubVersion={currentDeploymentTag}
              hubLoading={hubLoading}
              onGenerateKey={generateHubKeyPair}
              onRefreshDirectory={refreshHubDirectory}
              onNotify={setHubMessage}
            />
          )}

          {isAdmin && <DiscordBotPanel onNotify={setHubMessage} onOpenIncidents={() => setActiveView('incidents')} />}

          {isAdmin && (
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
                <div className="text-console-muted">Current: <span className="text-console-text">{currentDeploymentTag}</span></div>
                <div className="text-console-muted">Latest: <span className={updateStatusClass}>{latestUpdateTag || 'unknown'}</span></div>
                <div className="text-console-muted">Namespace: <span className="text-console-text">{updateInfo?.latest?.imageNamespace || 'unknown'}</span></div>
                <div className="text-console-muted">Status: <span className={updateStatusClass}>{updateStatusLabel}</span></div>
              </div>
              {hasUpdateAvailable && updateInfo?.latest && (
                <div className="border border-console-amber rounded p-2 text-[11px] text-console-muted">
                  Set <span className="text-console-text">IMAGE_NAMESPACE={updateInfo.latest.imageNamespace}</span> and <span className="text-console-text">IMAGE_TAG={updateInfo.latest.imageTag}</span>, then redeploy with image pull enabled.
                </div>
              )}
              {updateInfo?.error && <div className="text-[11px] text-console-error">{updateInfo.error}</div>}
            </div>
          )}

          {isAdmin && (
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
                      const disabledPeer = peer.status === 'disabled'
                      return (
                        <tr key={peer.id} className="border-b border-console-border/70">
                          <td className="py-2 px-2">
                            <div className="text-console-text">{peer.name || peer.hubId}</div>
                            <div className="text-[10px] text-console-muted break-all">{peer.publicUrl || peer.hubId}</div>
                            {peer.region && <div className="text-[10px] text-console-muted">{peer.region}</div>}
                          </td>
                          <td className={`py-2 px-2 uppercase ${connectedPeer ? 'text-console-accent' : 'text-console-muted'}`}>{peer.status}</td>
                          <td className="py-2 px-2 uppercase text-console-muted">{peer.status === 'connected' ? 'bidirectional' : peer.direction}</td>
                          <td className="py-2 px-2 text-console-muted tabular-nums">{peer.lastSeenAt ? fmtDateTime(peer.lastSeenAt) : '—'}</td>
                          <td className="py-2 px-2">
                            <div className="flex items-center gap-2">
                              {disabledPeer && (
                                <button
                                  onClick={() => enableHubPeer(peer.id)}
                                  className="text-[10px] px-1.5 py-0.5 border border-console-accent text-console-accent rounded hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
                                  disabled={hubPeerActionID === peer.id}
                                >
                                  {hubPeerActionID === peer.id ? '...' : 'ENABLE'}
                                </button>
                              )}
                              <button
                              onClick={() => deleteHubPeer(peer.id)}
                                className="text-[10px] px-1.5 py-0.5 border border-console-error text-console-error rounded hover:bg-console-error hover:bg-opacity-10 disabled:opacity-50"
                              disabled={hubPeerActionID === peer.id}
                              >
                              {hubPeerActionID === peer.id ? '...' : 'REMOVE'}
                              </button>
                            </div>
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

          {isAdmin && (
            <div className="border border-console-border rounded p-3 flex flex-col gap-3">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <div>
                  <p className="console-label text-xs">PEER INVITES</p>
                  <p className="text-[11px] text-console-muted">Generate a 7-day token for another SignalForge Hub.</p>
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
      </AuthenticatedView>

      <AccountView
        activeView={activeView}
        authUser={authUser}
        authLoading={authLoading}
        authEmail={authEmail}
        authToken={authToken}
        authMessage={authMessage}
        authError={authError}
        awaitingMagicLink={awaitingMagicLink}
        authCapabilities={authCapabilities}
        authPassword={authPassword}
        users={users}
        setUsers={setUsers}
        usersLoading={usersLoading}
        userActionID={userActionID}
        auditLogs={auditLogs}
        auditLoading={auditLoading}
        onAuthEmailChange={setAuthEmail}
        onAuthTokenChange={setAuthToken}
        onAuthPasswordChange={setAuthPassword}
        onRequestMagicLink={requestMagicLink}
        onVerifyMagicLinkToken={verifyMagicLinkToken}
        onPasswordLogin={passwordLogin}
        onLogoutSession={logoutSession}
        onRefreshUsers={refreshUsers}
        onSaveUser={saveUser}
        onApproveUser={approveUser}
        onSetUserPassword={setUserPassword}
        onRemoveUser={removeUser}
        onRefreshAuditLogs={refreshAuditLogs}
        onOpenHub={() => setActiveView('hub')}
      />

      <footer className="text-xs text-console-muted border-t border-console-border pt-3 flex flex-col gap-1">
        <div>projectseven .Co .Ltd © {new Date().getFullYear()} — ALL SYSTEMS OPERATIONAL</div>
        <div className="text-[10px] tabular-nums break-all">{footerDeploymentLabel}</div>
      </footer>
    </div>
  )
}

export default App
