import { pttUploadFilename } from './pttRecording'

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly body?: unknown,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export async function request<TResponse>(path: string, init?: RequestInit): Promise<TResponse> {
  const headers = new Headers(init?.headers)
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(path, {
    ...init,
    headers,
  })

  const isJSON = response.headers.get('content-type')?.includes('application/json')
  const body = isJSON ? await response.json() : await response.text()

  if (!response.ok) {
    throw new ApiError(`Request failed: ${response.status}`, response.status, body)
  }

  return body as TResponse
}

export type Call = {
  id: number
  userId?: string
  sourceId?: string
  redacted?: boolean
  dateTime: number
  system: number
  systemLabel: string
  talkgroup: number
  talkgroupLabel: string
  talkgroupGroup: string
  talkgroupTag: string
  frequency: number
  duration: number
  audioName: string
  audioType: string
  transcriptText?: string
  transcriptStatus?: string
  transcriptProvider?: string
  origin?: string
  senderUserId?: string
  senderEmail?: string
  createdAt: number
}

export type HubIdentity = {
  hubId: string
  name: string
  publicUrl: string
  region: string
  contact: string
  publicKey: string
  federationEnabled: boolean
  directoryValidationStatus: string
  trustLevel: string
  trustIssuerHubId: string
  trustCertificate: string
  trustExpiresAt: number
  trustVerifiedAt: number
  incidentManagementEnabled?: boolean
  incidentHandlerHubId?: string
  incidentAutoSuggest?: boolean
  incidentAutoOpen?: boolean
  incidentWatchAreas?: string[]
  incidentWatchPointLat?: number
  incidentWatchPointLon?: number
  createdAt: number
  updatedAt: number
}

export type IncidentTemplate = {
  id: string
  name: string
  incidentType: string
  selectionMode: RadioSetSelectionMode
  talkgroups: number[]
  talkgroupGroups: string[]
  defaultExposure: string
  defaultPriority: string
  nwsEventPatterns: string[]
  createdAt: number
  updatedAt: number
}

export type Incident = {
  id: string
  title: string
  incidentType: string
  status: string
  priority: string
  exposure: string
  radioSetId?: string
  templateId?: string
  openedByUserId?: string
  notes: string
  openedAt: number
  closedAt: number
  archivedAt: number
  createdAt: number
  updatedAt: number
  shareUrl?: string
  radioSet?: {
    name: string
    selectionMode: string
    talkgroups?: number[]
    talkgroupGroups?: string[]
  }
}

export type IncidentDiscordIntegration = {
  id: string
  incidentId: string
  kind: string
  status: string
  config?: {
    incidentTitle?: string
    exposure?: string
    publicPlayerUrl?: string
    voiceChannelId?: string
    textChannelId?: string
    categoryId?: string
    error?: string
  }
  createdAt: number
  updatedAt: number
}

export type IncidentSignal = {
  id: string
  source: string
  externalId: string
  eventType: string
  severity: string
  title: string
  detail: string
  templateId?: string
  incidentId?: string
  receivedAt: number
}

export type DiscordBotStatus = {
  botUserTag: string
  guildId: string
  guildName: string
  commandCount: number
  welcomeEnabled: boolean
  lastSeenAt: number
}

export type DiscordBotStatusResponse = {
  configured: boolean
  online: boolean
  status?: DiscordBotStatus
  pendingTasks?: number
  activeTasks?: number
  failedTasks?: number
}

export type IncidentSettings = {
  incidentManagementEnabled: boolean
  incidentHandlerHubId: string
  incidentAutoSuggest: boolean
  incidentAutoOpen: boolean
  incidentWatchAreas: string[]
  incidentWatchPointLat: number
  incidentWatchPointLon: number
  incidentWatchRadiusKm: number
}

export type CreateIncidentResponse = {
  incident: Incident
  radioSet?: RadioSet
  shareUrl?: string
  discordQueued?: boolean
  discordSkipReason?: string
}

export type HubInvite = {
  id: string
  token: string
  createdByUserId?: string
  expiresAt: number
  usedAt: number
  revokedAt: number
  createdAt: number
}

export type HubPeer = {
  id: string
  hubId: string
  name: string
  publicUrl: string
  region: string
  contact: string
  status: string
  direction: string
  acceptedAt: number
  lastSeenAt: number
  createdAt: number
  updatedAt: number
}

export type FederationStatus = {
  hub: HubIdentity
  peers: HubPeer[]
  sharedSources: IngestionSource[]
  exportableCallCount: number
  importedSourceCount: number
  importedCallCount: number
  pullPeerCount: number
  peerStatuses: FederationPeerStatus[]
  warnings: string[]
}

export type FederationPeerStatus = {
  peerId: string
  hubId: string
  name: string
  publicUrl: string
  canPull: boolean
  remoteSharedSources: number
  remoteSampleCalls: number
  error?: string
}

export type CallQuery = {
  limit?: number
  offset?: number
  sort?: 'datetime' | 'duration' | 'frequency' | 'talkgroup'
  order?: 'asc' | 'desc'
  q?: string
  group?: string
  groups?: string[]
  talkgroups?: number[]
}

export type TalkgroupSetting = {
  talkgroup: number
  favorite: boolean
  muted: boolean
  transcribe: boolean
  updatedAt: number
}

export type IngestionSource = {
  id: string
  userId?: string
  label: string
  enabled: boolean
  isShared: boolean
  systemId: number
  systemLabel: string
  lastSeenUnix: number
  errorCount: number
  callsReceived: number
  updatedAt: number
}

export type SourceAPIKey = {
  id: string
  sourceId: string
  userId?: string
  apiKey: string
  createdAt: number
  lastUsedAt: number
  revokedAt?: number
}

export type SourceSharesResponse = {
  userIds: string[]
}

export type AuthUser = {
  id: string
  email: string
  role: 'admin' | 'user' | 'guest'
  txEnabled?: boolean
  dispatcherEnabled?: boolean
}

export type UserRecord = {
  id: string
  email: string
  role: 'admin' | 'user' | 'guest'
  status: 'active' | 'pending' | 'disabled'
  txEnabled: boolean
  dispatcherEnabled: boolean
  passwordConfigured: boolean
  createdAt: number
  updatedAt: number
}

export type AuditLogEntry = {
  id: number
  userId?: string
  userEmail?: string
  action: string
  targetType: string
  targetId: string
  targetEmail?: string
  metadata: Record<string, unknown>
  createdAt: number
}

export type CallStorageStats = {
  callCount: number
  audioBytes: number
  oldestCallAt: number
  newestCallAt: number
  retentionDays: number
  retentionDaysFromEnv?: boolean
  archiveDir: string
  archiveDirFromEnv?: boolean
  archiveS3Uri: string
  archiveS3Cfg: string
  archiveDeleteLocalAfterS3: boolean
  archiveLoopEnabled: boolean
}

export type ArchiveCallsResult = {
  cutoffUnix: number
  archived: number
  deleted: number
  freedBytes: number
  archiveDir: string
  s3Uri?: string
  s3DirsSynced: number
  localRemoved: boolean
  vacuumQueued?: boolean
  dryRun: boolean
  remainingOld: number
  batches?: number
  batchLimit?: number
  stoppedEarly?: boolean
  note?: string
}

export type ArchiveJobStatus = ArchiveCallsResult & {
  running: boolean
  phase: string
  statusLine: string
  error?: string
  startedAt?: number
  updatedAt?: number
  lastBatchSize?: number
  initialRemaining?: number
}

export type TalkgroupInfo = {
  talkgroup: number
  talkgroupLabel: string
  talkgroupGroup: string
  systemLabel: string
}

export type RadioSetSelectionMode = 'talkgroups' | 'groups'

export type RadioSet = {
  id: string
  userId?: string
  name: string
  selectionMode?: RadioSetSelectionMode
  talkgroups: number[]
  talkgroupGroups?: string[]
  sourceIds?: string[]
  shareToken?: string
  pttTalkgroup?: number
  createdAt: number
  updatedAt: number
  incidentId?: string
  incidentStatus?: string
  incidentTitle?: string
}

function normalizeIncidentListItem(item: unknown): Incident | null {
  if (!item || typeof item !== 'object') return null
  const row = item as Record<string, unknown>
  const nested = row.incident
  if (nested && typeof nested === 'object') {
    const inc = nested as Incident
    return {
      ...inc,
      shareUrl: typeof row.shareUrl === 'string' ? row.shareUrl : inc.shareUrl,
      radioSet:
        row.radioSet && typeof row.radioSet === 'object'
          ? (row.radioSet as Incident['radioSet'])
          : inc.radioSet,
    }
  }
  if (typeof row.id === 'string' && typeof row.title === 'string') {
    return row as unknown as Incident
  }
  return null
}

export function parseIncidentsList(body: unknown): Incident[] {
  const raw: unknown[] = []
  if (Array.isArray(body)) {
    raw.push(...body)
  } else if (body && typeof body === 'object') {
    const wrapped = body as { incidents?: unknown; data?: unknown }
    if (Array.isArray(wrapped.incidents)) raw.push(...wrapped.incidents)
    else if (Array.isArray(wrapped.data)) raw.push(...wrapped.data)
  }
  return raw.map(normalizeIncidentListItem).filter((inc): inc is Incident => inc !== null)
}

export type AuthCapabilities = {
  passwordLoginEnabled: boolean
  magicLinkEnabled: boolean
  emailDeliveryConfigured: boolean
  autoApproveUsers: boolean
}

export type MagicLinkRequestResponse = {
  status: 'ok' | 'pending'
  user: AuthUser
  token?: string
  code?: string
  verifyUrl?: string
}

export type AuthSessionResponse = {
  status: string
  user: AuthUser
}

export type AuthMeResponse = {
  user: AuthUser
  sessionExpiresAt: number
}

export type VersionInfo = {
  version: string
  commit: string
  buildDate: string
  environment: string
  stackId: string
  deployTag: string
  transcriptionEnabled?: boolean
  passwordLoginEnabled?: boolean
  emailDeliveryConfigured?: boolean
}

export type UpdateManifest = {
  product: string
  channel: string
  version: string
  commit: string
  shortCommit: string
  imageRegistry: string
  imageNamespace: string
  imageTag: string
  publishedAt: string
  images: Record<string, string>
}

export type UpdateCheckResponse = {
  current: Record<string, string>
  latest?: UpdateManifest
  updateAvailable: boolean
  checkUrl: string
  error?: string
}

function buildQuery(params: CallQuery = {}): string {
  const query = new URLSearchParams()
  if (params.limit !== undefined) query.set('limit', String(params.limit))
  if (params.offset !== undefined) query.set('offset', String(params.offset))
  if (params.sort) query.set('sort', params.sort)
  if (params.order) query.set('order', params.order)
  if (params.q) query.set('q', params.q)
  if (params.group) query.set('group', params.group)
  if (params.groups && params.groups.length > 0) query.set('groups', params.groups.join(','))
  if (params.talkgroups && params.talkgroups.length > 0) query.set('talkgroups', params.talkgroups.join(','))
  const s = query.toString()
  return s ? `?${s}` : ''
}

export const api = {
  health: () => request<{ status: string; timestamp: string }>('/api/v1/health'),
  version: () => request<VersionInfo>('/api/v1/version'),
  updateCheck: () => request<UpdateCheckResponse>('/api/v1/update-check'),
  authCapabilities: () => request<AuthCapabilities>('/api/v1/auth/capabilities'),
  passwordLogin: (email: string, password: string) =>
    request<AuthSessionResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),
  requestMagicLink: (email: string) =>
    request<MagicLinkRequestResponse>('/api/v1/auth/magic-link', {
      method: 'POST',
      body: JSON.stringify({ email }),
    }),
  verifyMagicLink: (token: string) =>
    request<AuthSessionResponse>(`/api/v1/auth/verify?token=${encodeURIComponent(token)}`),
  verifyMagicCode: (email: string, code: string) =>
    request<AuthSessionResponse>('/api/v1/auth/verify-code', {
      method: 'POST',
      body: JSON.stringify({ email, code }),
    }),
  logout: () => request<{ status: string }>('/api/v1/auth/logout', { method: 'POST' }),
  refreshSession: () =>
    request<{ sessionExpiresAt: number }>('/api/v1/auth/refresh', { method: 'POST' }),
  me: () => request<AuthMeResponse>('/api/v1/auth/me'),
  users: () => request<UserRecord[]>('/api/v1/users'),
  updateUser: (
    id: string,
    payload: Pick<UserRecord, 'role' | 'status'> & {
      txEnabled?: boolean
      dispatcherEnabled?: boolean
    },
  ) =>
    request<{ status: string }>(`/api/v1/users/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    }),
  deleteUser: (id: string) =>
    request<{ status: string }>(`/api/v1/users/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  setUserPassword: (id: string, password: string) =>
    request<{ status: string }>(`/api/v1/users/${encodeURIComponent(id)}/password`, {
      method: 'PUT',
      body: JSON.stringify({ password }),
    }),
  auditLogs: (limit = 100) => request<AuditLogEntry[]>(`/api/v1/audit-logs?limit=${encodeURIComponent(String(limit))}`),
  hubIdentity: () => request<HubIdentity>('/api/v1/hub/identity'),
  updateHubIdentity: (identity: Pick<HubIdentity, 'name' | 'publicUrl' | 'region' | 'contact' | 'federationEnabled'>) =>
    request<HubIdentity>('/api/v1/hub/identity', {
      method: 'PUT',
      body: JSON.stringify(identity),
    }),
  generateHubKeyPair: () => request<HubIdentity>('/api/v1/hub/identity/keypair', { method: 'POST' }),
  refreshHubDirectory: () => request<HubIdentity>('/api/v1/hub/directory/refresh', { method: 'POST' }),
  discordStatus: () => request<DiscordBotStatusResponse>('/api/v1/discord/status'),
  reconcileDiscordIncidents: () =>
    request<{
      queued: number
      retried: number
      skipped: number
      pendingTasks: number
      activeTasks: number
      failedTasks: number
    }>('/api/v1/discord/reconcile-incidents', { method: 'POST' }),
  incidentSettings: () => request<IncidentSettings>('/api/v1/hub/incidents/settings'),
  updateIncidentSettings: (settings: IncidentSettings) =>
    request<HubIdentity>('/api/v1/hub/incidents/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
  incidentTemplates: () => request<IncidentTemplate[]>('/api/v1/incident-templates'),
  incidents: async (archived = false) => {
    const body = await request<unknown>(`/api/v1/incidents${archived ? '?archived=1' : ''}`)
    return parseIncidentsList(body)
  },
  createIncident: (payload: {
    title: string
    templateId?: string
    type?: string
    priority?: string
    exposure?: string
    notes?: string
    activate?: boolean
    talkgroups?: number[]
    talkgroupGroups?: string[]
  }) =>
    request<CreateIncidentResponse>('/api/v1/incidents', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  incidentSignals: () => request<IncidentSignal[]>('/api/v1/incidents/signals'),
  pollIncidentSignals: () =>
    request<{ processed: number }>('/api/v1/incidents/signals/poll', { method: 'POST' }),
  promoteIncidentSignal: (id: string) =>
    request<CreateIncidentResponse>(`/api/v1/incidents/signals/${encodeURIComponent(id)}/promote`, {
      method: 'POST',
    }),
  activateIncident: (id: string) =>
    request<CreateIncidentResponse>(`/api/v1/incidents/${encodeURIComponent(id)}/activate`, { method: 'POST' }),
  closeIncident: (id: string) =>
    request<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}/close`, { method: 'POST' }),
  closeIncidentByRadioSet: (radioSetId: string) =>
    request<Incident>(`/api/v1/radio-sets/${encodeURIComponent(radioSetId)}/close-incident`, { method: 'POST' }),
  archiveIncident: (id: string) =>
    request<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}/archive`, { method: 'POST' }),
  incidentDiscordIntegration: (id: string) =>
    request<{ integration?: IncidentDiscordIntegration }>(
      `/api/v1/incidents/${encodeURIComponent(id)}/integrations/discord`,
    ),
  createIncidentDiscordIntegration: (id: string) =>
    request<{ integration?: IncidentDiscordIntegration }>(
      `/api/v1/incidents/${encodeURIComponent(id)}/integrations/discord`,
      { method: 'POST' },
    ),
  deleteIncidentDiscordIntegration: (id: string) =>
    request<void>(`/api/v1/incidents/${encodeURIComponent(id)}/integrations/discord`, {
      method: 'DELETE',
    }),
  hubInvites: () => request<HubInvite[]>('/api/v1/hub/invites'),
  createHubInvite: () => request<HubInvite>('/api/v1/hub/invites', { method: 'POST' }),
  revokeHubInvite: (id: string) => request<HubInvite>(`/api/v1/hub/invites/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  federationStatus: () => request<FederationStatus>('/api/v1/hub/federation/status'),
  hubPeers: () => request<HubPeer[]>('/api/v1/hub/peers'),
  connectHubPeer: (remoteUrl: string, inviteToken: string) =>
    request<HubPeer>('/api/v1/hub/peers', {
      method: 'POST',
      body: JSON.stringify({ remoteUrl, inviteToken }),
    }),
  enableHubPeer: (id: string) => request<HubPeer>(`/api/v1/hub/peers/${encodeURIComponent(id)}/enable`, { method: 'PATCH' }),
  deleteHubPeer: (id: string) => request<void>(`/api/v1/hub/peers/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  calls: (params?: CallQuery) => request<Call[]>(`/api/v1/calls${buildQuery(params ?? { limit: 100 })}`),
  callGroups: () => request<string[]>('/api/v1/calls/groups'),
  talkgroupSettings: () => request<TalkgroupSetting[]>('/api/v1/talkgroups/settings'),
  updateTalkgroupSettings: (talkgroup: number, setting: { favorite: boolean; muted: boolean; transcribe: boolean }) =>
    request<TalkgroupSetting>(`/api/v1/talkgroups/${talkgroup}/settings`, {
      method: 'PUT',
      body: JSON.stringify(setting),
    }),
  deleteTalkgroup: (talkgroup: number) =>
    request<{ status: string; talkgroup: number; callsDeleted: number }>(`/api/v1/talkgroups/${talkgroup}`, {
      method: 'DELETE',
    }),
  ingestionSources: () => request<IngestionSource[]>('/api/v1/sources'),
  updateIngestionSource: (id: string, source: Partial<IngestionSource>) =>
    request<IngestionSource>(`/api/v1/sources?id=${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(source),
    }),
  deleteIngestionSource: (id: string) =>
    request<{ status: string }>(`/api/v1/sources/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  generateSourceKey: (sourceId: string) =>
    request<SourceAPIKey>(`/api/v1/sources/${encodeURIComponent(sourceId)}/keys`, {
      method: 'POST',
    }),
  listSourceKeys: (sourceId: string) =>
    request<SourceAPIKey[]>(`/api/v1/sources/${encodeURIComponent(sourceId)}/keys`),
  revokeSourceKey: (sourceId: string, keyId: string) =>
    request<{ status: string }>(`/api/v1/sources/${encodeURIComponent(sourceId)}/keys/${encodeURIComponent(keyId)}`, {
      method: 'DELETE',
    }),
  sourceShares: (sourceId: string) =>
    request<SourceSharesResponse>(`/api/v1/sources/${encodeURIComponent(sourceId)}/shares`),
  updateSourceShares: (sourceId: string, userIds: string[]) =>
    request<SourceSharesResponse>(`/api/v1/sources/${encodeURIComponent(sourceId)}/shares`, {
      method: 'PUT',
      body: JSON.stringify({ userIds }),
    }),
  distinctTalkgroups: () => request<TalkgroupInfo[]>('/api/v1/talkgroups/distinct'),
  radioSets: () => request<RadioSet[]>('/api/v1/radio-sets'),
  createRadioSet: (
    name: string,
    selectionMode: RadioSetSelectionMode,
    talkgroups: number[],
    talkgroupGroups: string[],
  ) =>
    request<RadioSet>('/api/v1/radio-sets', {
      method: 'POST',
      body: JSON.stringify({ name, selectionMode, talkgroups, talkgroupGroups }),
    }),
  updateRadioSet: (
    id: string,
    name: string,
    selectionMode: RadioSetSelectionMode,
    talkgroups: number[],
    talkgroupGroups: string[],
  ) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify({ name, selectionMode, talkgroups, talkgroupGroups }),
    }),
  deleteRadioSet: (id: string) =>
    request<{ status: string }>(`/api/v1/radio-sets/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  generateShareToken: (id: string) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}/share`, { method: 'POST' }),
  revokeShareToken: (id: string) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}/share`, { method: 'DELETE' }),
  callStorageStats: () => request<CallStorageStats>('/api/v1/admin/calls/storage'),
  archiveCalls: (body: { olderThanDays?: number; dryRun?: boolean; limit?: number; untilEmpty?: boolean; async?: boolean }) =>
    request<ArchiveCallsResult | ArchiveJobStatus>('/api/v1/admin/calls/archive', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  archiveJobStatus: () => request<ArchiveJobStatus>('/api/v1/admin/calls/archive/status'),
  uploadPTT: async (
    id: string,
    audio: Blob,
    durationSeconds: number,
    clientId: string,
  ): Promise<PTTUploadResponse> => {
    const form = new FormData()
    form.append('audio', audio, pttUploadFilename(clientId, audio.type))
    form.append('duration', String(durationSeconds))
    form.append('clientId', clientId)
    const response = await fetch(`/api/v1/radio-sets/${encodeURIComponent(id)}/ptt`, {
      method: 'POST',
      body: form,
    })
    const isJSON = response.headers.get('content-type')?.includes('application/json')
    const body = isJSON ? await response.json() : await response.text()
    if (!response.ok) {
      throw new ApiError(`PTT upload failed: ${response.status}`, response.status, body)
    }
    return body as PTTUploadResponse
  },
  uploadPTTBroadcast: async (
    radioSetIds: string[],
    audio: Blob,
    durationSeconds: number,
    clientId: string,
  ): Promise<PTTBroadcastResponse> => {
    const form = new FormData()
    form.append('audio', audio, pttUploadFilename(clientId, audio.type))
    form.append('duration', String(durationSeconds))
    form.append('clientId', clientId)
    form.append('radioSetIds', radioSetIds.join(','))
    const response = await fetch('/api/v1/ptt/broadcast', {
      method: 'POST',
      body: form,
    })
    const isJSON = response.headers.get('content-type')?.includes('application/json')
    const body = isJSON ? await response.json() : await response.text()
    if (!response.ok) {
      throw new ApiError(`PTT broadcast failed: ${response.status}`, response.status, body)
    }
    return body as PTTBroadcastResponse
  },
}

export type PTTUploadResponse = {
  callId: number
  talkgroup: number
}

export type PTTBroadcastResult = {
  radioSetId: string
  callId?: number
  talkgroup?: number
  error?: string
}

export type PTTBroadcastResponse = {
  results: PTTBroadcastResult[]
}
