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
  createdAt: number
  updatedAt: number
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
  talkgroups?: number[]
}

export type TalkgroupSetting = {
  talkgroup: number
  favorite: boolean
  muted: boolean
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
}

export type UserRecord = {
  id: string
  email: string
  role: 'admin' | 'user' | 'guest'
  status: 'active' | 'pending' | 'disabled'
  createdAt: number
  updatedAt: number
}

export type AuditLogEntry = {
  id: number
  userId?: string
  action: string
  targetType: string
  targetId: string
  metadata: Record<string, unknown>
  createdAt: number
}

export type TalkgroupInfo = {
  talkgroup: number
  talkgroupLabel: string
  talkgroupGroup: string
}

export type RadioSet = {
  id: string
  userId?: string
  name: string
  talkgroups: number[]
  sourceIds?: string[]
  shareToken?: string
  createdAt: number
  updatedAt: number
}

export type MagicLinkRequestResponse = {
  status: 'ok' | 'pending'
  user: AuthUser
  token?: string
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
  if (params.talkgroups && params.talkgroups.length > 0) query.set('talkgroups', params.talkgroups.join(','))
  const s = query.toString()
  return s ? `?${s}` : ''
}

export const api = {
  health: () => request<{ status: string; timestamp: string }>('/api/v1/health'),
  version: () => request<VersionInfo>('/api/v1/version'),
  updateCheck: () => request<UpdateCheckResponse>('/api/v1/update-check'),
  requestMagicLink: (email: string) =>
    request<MagicLinkRequestResponse>('/api/v1/auth/magic-link', {
      method: 'POST',
      body: JSON.stringify({ email }),
    }),
  verifyMagicLink: (token: string) =>
    request<AuthSessionResponse>(`/api/v1/auth/verify?token=${encodeURIComponent(token)}`),
  logout: () => request<{ status: string }>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<AuthMeResponse>('/api/v1/auth/me'),
  users: () => request<UserRecord[]>('/api/v1/users'),
  updateUser: (id: string, payload: Pick<UserRecord, 'role' | 'status'>) =>
    request<{ status: string }>(`/api/v1/users/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    }),
  deleteUser: (id: string) =>
    request<{ status: string }>(`/api/v1/users/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  auditLogs: (limit = 100) => request<AuditLogEntry[]>(`/api/v1/audit-logs?limit=${encodeURIComponent(String(limit))}`),
  hubIdentity: () => request<HubIdentity>('/api/v1/hub/identity'),
  updateHubIdentity: (identity: Pick<HubIdentity, 'name' | 'publicUrl' | 'region' | 'contact' | 'federationEnabled'>) =>
    request<HubIdentity>('/api/v1/hub/identity', {
      method: 'PUT',
      body: JSON.stringify(identity),
    }),
  refreshHubDirectory: () => request<HubIdentity>('/api/v1/hub/directory/refresh', { method: 'POST' }),
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
  updateTalkgroupSettings: (talkgroup: number, setting: { favorite: boolean; muted: boolean }) =>
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
  createRadioSet: (name: string, talkgroups: number[]) =>
    request<RadioSet>('/api/v1/radio-sets', { method: 'POST', body: JSON.stringify({ name, talkgroups }) }),
  updateRadioSet: (id: string, name: string, talkgroups: number[]) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify({ name, talkgroups }),
    }),
  deleteRadioSet: (id: string) =>
    request<{ status: string }>(`/api/v1/radio-sets/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  generateShareToken: (id: string) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}/share`, { method: 'POST' }),
  revokeShareToken: (id: string) =>
    request<RadioSet>(`/api/v1/radio-sets/${encodeURIComponent(id)}/share`, { method: 'DELETE' }),
}
