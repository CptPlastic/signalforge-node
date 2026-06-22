import { useCallback, useEffect, useState } from 'react'
import {
  api,
  ApiError,
  type AuthUser,
  type HubPeer,
  type Incident,
  type IncidentDiscordIntegration,
  type IncidentSettings,
  type IncidentSignal,
  type IncidentTemplate,
  type CreateIncidentResponse,
  type RadioSet,
} from '../../lib/api'
import { fmtDateTime } from '../../lib/format'

type Props = Readonly<{
  authUser: AuthUser
  isAdmin: boolean
  hubPeers: HubPeer[]
  onNotify: (message: string) => void
  onOpenRadioSet?: (radioSetId: string) => void
}>

type IncidentTab = 'active' | 'archive'

const EXPOSURES = ['members', 'community', 'internal'] as const
const PRIORITIES = ['low', 'normal', 'high', 'urgent'] as const

function incidentFromCreateResponse(resp: CreateIncidentResponse): Incident {
  return {
    ...resp.incident,
    shareUrl: resp.shareUrl,
    radioSet: resp.radioSet
      ? {
          name: resp.radioSet.name,
          selectionMode: resp.radioSet.selectionMode ?? 'groups',
          talkgroups: resp.radioSet.talkgroups,
          talkgroupGroups: resp.radioSet.talkgroupGroups,
        }
      : undefined,
  }
}

function mergeRunningIncident(list: Incident[], inc: Incident): Incident[] {
  const rest = list.filter((i) => i.id !== inc.id)
  if (inc.status === 'active' || inc.status === 'draft' || inc.status === 'monitoring') {
    return [inc, ...rest]
  }
  return rest
}

function incidentsFromRadioSets(sets: RadioSet[]): Incident[] {
  const out: Incident[] = []
  for (const rs of sets) {
    if (!rs.incidentId) continue
    const status = rs.incidentStatus ?? ''
    if (status !== 'active' && status !== 'draft' && status !== 'monitoring') continue
    out.push({
      id: rs.incidentId,
      title: rs.incidentTitle ?? rs.name.replace(/^INC ·\s*/, ''),
      incidentType: 'custom',
      status,
      priority: 'normal',
      exposure: 'members',
      radioSetId: rs.id,
      notes: '',
      openedAt: rs.updatedAt,
      closedAt: 0,
      archivedAt: 0,
      createdAt: rs.createdAt,
      updatedAt: rs.updatedAt,
      radioSet: {
        name: rs.name,
        selectionMode: rs.selectionMode ?? 'groups',
        talkgroups: rs.talkgroups,
        talkgroupGroups: rs.talkgroupGroups,
      },
    })
  }
  return out
}

function mergeIncidentLists(primary: Incident[], fallback: Incident[]): Incident[] {
  const seen = new Set(primary.map((i) => i.id))
  const merged = [...primary]
  for (const inc of fallback) {
    if (!seen.has(inc.id)) {
      seen.add(inc.id)
      merged.push(inc)
    }
  }
  return merged
}

export function IncidentsPanel({ authUser, isAdmin, hubPeers, onNotify, onOpenRadioSet }: Props) {
  const canManage = isAdmin || !!authUser.dispatcherEnabled

  const [settings, setSettings] = useState<IncidentSettings | null>(null)
  const [settingsDraft, setSettingsDraft] = useState<IncidentSettings | null>(null)
  const [templates, setTemplates] = useState<IncidentTemplate[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [archivedIncidents, setArchivedIncidents] = useState<Incident[]>([])
  const [signals, setSignals] = useState<IncidentSignal[]>([])
  const [discordByIncident, setDiscordByIncident] = useState<Record<string, IncidentDiscordIntegration | null>>({})
  const [loading, setLoading] = useState(false)
  const [tab, setTab] = useState<IncidentTab>('active')
  const [templateId, setTemplateId] = useState('general')
  const [title, setTitle] = useState('')
  const [exposure, setExposure] = useState('members')
  const [priority, setPriority] = useState('normal')
  const [notes, setNotes] = useState('')
  const [activate, setActivate] = useState(true)
  const [showSignals, setShowSignals] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [apiTotal, setApiTotal] = useState<number | null>(null)

  const refresh = useCallback(async () => {
    if (!canManage) return
    setLoading(true)
    setLoadError('')
    try {
      const [s, t, sig] = await Promise.all([
        api.incidentSettings().catch((err) => {
          console.error(err)
          return null
        }),
        api.incidentTemplates().catch((err) => {
          console.error(err)
          onNotify('Could not load incident templates')
          return [] as IncidentTemplate[]
        }),
        api.incidentSignals().catch(() => [] as IncidentSignal[]),
      ])

      let all: Incident[] = []
      let listLoadError = ''
      try {
        all = await api.incidents(true)
        setApiTotal(all.length)
      } catch (err) {
        console.error(err)
        setApiTotal(null)
        if (err instanceof ApiError) {
          if (err.status === 403) {
            listLoadError =
              'Incidents API returned 403. Enable Incident management in INCIDENT SETTINGS (bottom), clear Incident handler hub ID unless a remote handler is online, and SAVE.'
          } else {
            listLoadError = `Incidents API error ${err.status}. Open browser devtools → Network → GET /api/v1/incidents`
          }
        } else {
          listLoadError = 'Could not load incidents from the hub API.'
        }
        all = []
      }
      setLoadError(listLoadError)

      if (s) {
        setSettings(s)
        setSettingsDraft(s)
      }
      setTemplates(t)
      let activeList = all.filter((i) => i.status === 'active' || i.status === 'draft' || i.status === 'monitoring')
      if (activeList.length === 0 && !listLoadError) {
        const sets = await api.radioSets().catch(() => [] as RadioSet[])
        const fromSets = incidentsFromRadioSets(sets)
        if (fromSets.length > 0) {
          activeList = mergeIncidentLists(activeList, fromSets)
          setLoadError(
            'Incident list API returned empty but open incidents exist on radio sets — showing those. Redeploy api + web after updating.',
          )
        }
      }
      setIncidents(activeList)
      setArchivedIncidents(all.filter((i) => i.status === 'closed' || i.status === 'archived'))
      setSignals(sig)
      if (activeList.length === 0 && sig.length > 0) {
        setShowSignals(true)
      }
      const monitoring = activeList.filter((i) => i.status === 'active' || i.status === 'monitoring')
      const discordEntries = await Promise.all(
        monitoring.map(async (inc) => {
          try {
            const resp = await api.incidentDiscordIntegration(inc.id)
            return [inc.id, resp.integration ?? null] as const
          } catch {
            return [inc.id, null] as const
          }
        }),
      )
      setDiscordByIncident(Object.fromEntries(discordEntries))
    } catch (err) {
      console.error(err)
      onNotify('Could not load incidents')
    } finally {
      setLoading(false)
    }
  }, [canManage, onNotify])

  useEffect(() => {
    if (templates.length === 0) return
    if (!templates.some((t) => t.id === templateId)) {
      setTemplateId(templates[0].id)
    }
  }, [templates, templateId])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    const tmpl = templates.find((t) => t.id === templateId)
    if (!tmpl) return
    setExposure(tmpl.defaultExposure || 'members')
    setPriority(tmpl.defaultPriority || 'normal')
  }, [templateId, templates])

  if (!canManage) {
    return (
      <div className="border border-console-border rounded p-3 text-xs text-console-muted">
        Incident management requires admin or dispatcher role.
      </div>
    )
  }

  async function saveSettings() {
    if (!settingsDraft || !isAdmin) return
    setLoading(true)
    try {
      await api.updateIncidentSettings(settingsDraft)
      onNotify('Incident settings saved')
      await refresh()
    } catch (err) {
      console.error(err)
      onNotify('Save failed')
    } finally {
      setLoading(false)
    }
  }

  async function createIncident() {
    if (!title.trim()) {
      onNotify('Title required')
      return
    }
    setLoading(true)
    try {
      const resp = await api.createIncident({
        title: title.trim(),
        templateId,
        exposure,
        priority,
        notes: notes.trim(),
        activate,
      })
      if (resp.discordQueued) {
        onNotify('Incident active — Discord rooms queued (bot creates channels within ~15s)')
      } else if (resp.discordSkipReason) {
        onNotify(`Incident active — Discord not queued: ${resp.discordSkipReason}`)
      } else if (resp.shareUrl) {
        onNotify('Incident active — player link ready')
      } else if (activate) {
        onNotify('Incident active — enable DISCORD_BOT_WORKER_TOKEN + online bot for auto Discord rooms')
      } else {
        onNotify('Incident created (draft — activate to open Discord rooms)')
      }
      const created = incidentFromCreateResponse(resp)
      setIncidents((prev) => mergeRunningIncident(prev, created))
      setApiTotal((n) => (n ?? 0) + 1)
      setTitle('')
      setNotes('')
      setShowCreate(false)
      void refresh()
    } catch (err) {
      console.error(err)
      onNotify('Create failed — is incident management enabled?')
    } finally {
      setLoading(false)
    }
  }

  async function pollSignals() {
    setLoading(true)
    try {
      const { processed } = await api.pollIncidentSignals()
      onNotify(`Polled NWS/IEM — ${processed} new signal(s)`)
      setShowSignals(true)
      await refresh()
    } catch (err) {
      console.error(err)
      onNotify('Poll failed')
    } finally {
      setLoading(false)
    }
  }

  function copyShareUrl(url: string) {
    void navigator.clipboard.writeText(url).then(
      () => onNotify('Public player link copied'),
      () => onNotify(url),
    )
  }

  function endIncident(inc: Incident) {
    if (!globalThis.confirm(`End incident "${inc.title}"?\n\nThis closes the incident, stops Discord rooms, and revokes the public player link.`)) {
      return
    }
    setLoading(true)
    api.closeIncident(inc.id)
      .then(() => { onNotify('Incident ended'); return refresh() })
      .catch(() => onNotify('End failed'))
      .finally(() => setLoading(false))
  }

  function renderIncidentRow(inc: Incident, actions: 'full' | 'archive-only') {
    const canEnd = actions === 'full' && (inc.status === 'active' || inc.status === 'draft' || inc.status === 'monitoring')
    const discord = discordByIncident[inc.id]
    const discordLabel = discord?.status === 'active'
      ? 'DISCORD LIVE'
      : discord?.status === 'pending'
        ? 'DISCORD PENDING'
        : discord?.status === 'stopping'
          ? 'DISCORD STOPPING'
          : discord?.status === 'failed'
            ? 'RETRY DISCORD'
            : 'DISCORD ROOMS'
    const monitorLabel =
      inc.radioSet?.selectionMode === 'groups'
        ? (inc.radioSet.talkgroupGroups ?? []).join(', ') || 'no groups'
        : (inc.radioSet?.talkgroups ?? []).map((tg) => String(tg)).join(', ') || 'no talkgroups'

    return (
      <div
        key={inc.id}
        className={`border rounded p-3 flex flex-col gap-2 ${
          inc.status === 'active' || inc.status === 'monitoring'
            ? 'border-console-accent/60 bg-console-accent/5'
            : 'border-console-border'
        }`}
      >
        <div className="flex justify-between gap-2 items-start flex-wrap">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-console-text font-semibold text-sm">{inc.title}</span>
              <span className="text-[10px] uppercase px-1.5 py-0.5 rounded border border-console-border text-console-muted">
                {inc.status}
              </span>
            </div>
            <div className="text-console-muted text-[10px] uppercase mt-1">
              {inc.priority} · {inc.exposure}
              {inc.openedAt ? ` · opened ${fmtDateTime(inc.openedAt)}` : ''}
              {discord?.status ? ` · discord ${discord.status}` : ''}
            </div>
            {inc.radioSet && canEnd && (
              <div className="text-console-muted text-[10px] mt-1">
                Radio set: {inc.radioSet.name} · {inc.radioSet.selectionMode === 'groups' ? 'groups' : 'TGs'}: {monitorLabel}
              </div>
            )}
            {discord?.config?.error && (
              <div className="text-[10px] text-console-error mt-1">{discord.config.error}</div>
            )}
          </div>
          <div className="flex gap-1 shrink-0 flex-wrap justify-end">
            {actions === 'full' && inc.status === 'draft' && (
              <button
                type="button"
                disabled={loading}
                onClick={() => {
                  setLoading(true)
                  api.activateIncident(inc.id)
                    .then((r) => {
                      if (r.discordQueued) onNotify('Incident activated — Discord rooms queued')
                      else if (r.discordSkipReason) onNotify(`Discord not queued: ${r.discordSkipReason}`)
                      else if (r.shareUrl) copyShareUrl(r.shareUrl)
                      else onNotify('Incident activated')
                      return refresh()
                    })
                    .catch(() => onNotify('Activate failed'))
                    .finally(() => setLoading(false))
                }}
                className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px]"
              >
                ACTIVATE
              </button>
            )}
            {canEnd && (
              <button
                type="button"
                disabled={loading}
                onClick={() => endIncident(inc)}
                className="px-3 py-1 border border-console-error text-console-error rounded text-[10px] font-semibold hover:bg-console-error/10"
              >
                END INCIDENT
              </button>
            )}
            {inc.status === 'closed' && (
              <button
                type="button"
                disabled={loading}
                onClick={() => {
                  setLoading(true)
                  api.archiveIncident(inc.id)
                    .then(() => { onNotify('Archived'); return refresh() })
                    .catch(() => onNotify('Archive failed'))
                    .finally(() => setLoading(false))
                }}
                className="px-2 py-1 border border-console-border rounded text-[10px]"
              >
                ARCHIVE
              </button>
            )}
          </div>
        </div>
        <div className="flex gap-2 flex-wrap">
          {inc.radioSetId && onOpenRadioSet && canEnd && (
            <button
              type="button"
              onClick={() => onOpenRadioSet(inc.radioSetId!)}
              className="px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
            >
              LISTEN IN HUB
            </button>
          )}
          {inc.shareUrl && (
            <>
              <a
                href={inc.shareUrl}
                target="_blank"
                rel="noreferrer"
                className="px-2 py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:text-console-accent"
              >
                PUBLIC PLAYER
              </a>
              <button
                type="button"
                onClick={() => copyShareUrl(inc.shareUrl!)}
                className="px-2 py-0.5 border border-console-border rounded text-[10px] text-console-muted hover:text-console-accent"
              >
                COPY LINK
              </button>
            </>
          )}
          {actions === 'full' && (inc.status === 'active' || inc.status === 'monitoring') && inc.exposure !== 'internal' && (
            <>
              {(discord?.status === 'active' || discord?.status === 'pending') ? (
                <button
                  type="button"
                  disabled={loading}
                  onClick={() => {
                    setLoading(true)
                    api.deleteIncidentDiscordIntegration(inc.id)
                      .then(() => { onNotify('Discord rooms stopping'); return refresh() })
                      .catch(() => onNotify('Stop Discord failed'))
                      .finally(() => setLoading(false))
                  }}
                  className="px-2 py-0.5 border border-console-border rounded text-[10px] text-console-muted hover:text-console-error"
                >
                  STOP DISCORD
                </button>
              ) : (
                <button
                  type="button"
                  disabled={loading}
                  onClick={() => {
                    setLoading(true)
                    api.createIncidentDiscordIntegration(inc.id)
                      .then(() => { onNotify('Discord rooms requested'); return refresh() })
                      .catch(async (err) => {
                        const msg = err instanceof Error ? err.message : 'Discord request failed'
                        onNotify(msg.includes('503') || msg.includes('not configured')
                          ? 'Discord not linked — set DISCORD_BOT_WORKER_TOKEN in stack env'
                          : msg)
                      })
                      .finally(() => setLoading(false))
                  }}
                  className="px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
                >
                  {discordLabel}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    )
  }

  const visibleIncidents = tab === 'active' ? incidents : archivedIncidents

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <p className="console-label text-xs">// INCIDENTS</p>
          <p className="text-[11px] text-console-muted">
            Top nav → <strong>INCIDENTS</strong>. Weather alerts are suggestions only — not running incidents until you OPEN one.
          </p>
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => setShowCreate((v) => !v)}
            className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent"
          >
            {showCreate ? 'HIDE FORM' : 'NEW INCIDENT'}
          </button>
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={loading}
            className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent disabled:opacity-50"
          >
            REFRESH
          </button>
        </div>
      </div>

      {!settings?.incidentManagementEnabled && (
        <p className="text-console-error text-[11px] border border-console-error/40 rounded p-2">
          Incident management is disabled. Admin: open <strong>INCIDENT SETTINGS</strong> at the bottom, enable it, and save.
        </p>
      )}

      {loadError && (
        <p className="text-console-error text-[11px] border border-console-error/40 rounded p-2">
          {loadError}
        </p>
      )}

      {apiTotal !== null && !loadError && (
        <p className="text-[10px] text-console-muted">
          API returned {apiTotal} incident record(s) · showing {incidents.length} running
        </p>
      )}

      {/* ── ACTIVE / CLOSED LIST (top) ── */}
      <div className="flex flex-col gap-2">
        <div className="flex gap-2 text-[10px] items-center flex-wrap">
          <button
            type="button"
            onClick={() => setTab('active')}
            className={`px-2 py-1 border rounded font-semibold ${tab === 'active' ? 'border-console-accent text-console-accent bg-console-accent/10' : 'border-console-border text-console-muted'}`}
          >
            RUNNING ({incidents.length})
          </button>
          <button
            type="button"
            onClick={() => setTab('archive')}
            className={`px-2 py-1 border rounded ${tab === 'archive' ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
          >
            ENDED ({archivedIncidents.length})
          </button>
        </div>

        {visibleIncidents.length === 0 ? (
          <div className="border border-dashed border-console-border rounded p-4 text-center text-[11px] text-console-muted">
            {tab === 'active'
              ? 'No running incidents. Open one from weather signals or use NEW INCIDENT.'
              : 'No ended incidents yet.'}
          </div>
        ) : (
          visibleIncidents.map((inc) => renderIncidentRow(inc, tab === 'active' ? 'full' : 'archive-only'))
        )}
      </div>

      {/* ── WEATHER SIGNALS (suggestions only) ── */}
      <div className="border border-console-border rounded p-2 flex flex-col gap-2">
        <button
          type="button"
          onClick={() => setShowSignals((v) => !v)}
          className="flex items-center justify-between gap-2 text-left w-full"
        >
          <div>
            <p className="console-label text-[10px]">WEATHER ALERTS (not incidents yet)</p>
            <p className="text-[10px] text-console-muted">
              {signals.length} pending · OPEN creates a running incident
            </p>
          </div>
          <span className="text-console-muted text-[10px]">{showSignals ? '▲' : '▼'}</span>
        </button>
        {showSignals && (
          <>
            <div className="flex justify-end">
              <button
                type="button"
                onClick={() => void pollSignals()}
                disabled={loading || !settings?.incidentManagementEnabled}
                className="text-[10px] text-console-muted hover:text-console-accent disabled:opacity-50"
              >
                POLL NWS/IEM
              </button>
            </div>
            {signals.length === 0 ? (
              <p className="text-console-muted text-[10px]">No pending weather alerts.</p>
            ) : (
              signals.slice(0, 8).map((sig) => (
                <div key={sig.id} className="border border-console-border/70 rounded p-2 flex justify-between gap-2 bg-console-bg/30">
                  <div>
                    <div className="text-console-text text-xs">{sig.title || sig.eventType}</div>
                    <div className="text-console-muted text-[10px]">{sig.source} · {sig.severity} · {fmtDateTime(sig.receivedAt)}</div>
                  </div>
                  <button
                    type="button"
                    disabled={loading}
                    onClick={() => {
                      setLoading(true)
                      api.promoteIncidentSignal(sig.id)
                        .then((r) => {
                          const created = incidentFromCreateResponse(r)
                      setIncidents((prev) => mergeRunningIncident(prev, created))
                      if (r.discordQueued) onNotify('Incident opened — Discord rooms queued')
                          else if (r.discordSkipReason) onNotify(`Discord not queued: ${r.discordSkipReason}`)
                          else if (r.shareUrl) copyShareUrl(r.shareUrl)
                          else onNotify('Incident opened from weather alert')
                          return refresh()
                        })
                        .catch(() => onNotify('Open failed'))
                        .finally(() => setLoading(false))
                    }}
                    className="shrink-0 px-2 py-1 border border-console-accent text-console-accent rounded text-[10px]"
                  >
                    OPEN INCIDENT
                  </button>
                </div>
              ))
            )}
          </>
        )}
      </div>

      {/* ── NEW INCIDENT FORM (collapsed by default) ── */}
      {showCreate && (
        <div className="border border-console-border rounded p-2 flex flex-col gap-2 text-xs">
          <p className="console-label text-[10px]">NEW INCIDENT (manual)</p>
          <select
            value={templateId}
            onChange={(e) => setTemplateId(e.target.value)}
            className="w-full bg-console-bg border border-console-border rounded px-2 py-1"
          >
            {templates.length === 0 ? (
              <option value="">No templates — click REFRESH</option>
            ) : (
              templates.map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))
            )}
          </select>
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Severe Weather — Yukon"
            className="w-full bg-console-bg border border-console-border rounded px-2 py-1 outline-none focus:border-console-accent"
          />
          <div className="grid gap-2 md:grid-cols-2">
            <div>
              <p className="text-console-muted mb-1">Exposure</p>
              <select
                value={exposure}
                onChange={(e) => setExposure(e.target.value)}
                className="w-full bg-console-bg border border-console-border rounded px-2 py-1"
              >
                {EXPOSURES.map((e) => (
                  <option key={e} value={e}>{e}</option>
                ))}
              </select>
            </div>
            <div>
              <p className="text-console-muted mb-1">Priority</p>
              <select
                value={priority}
                onChange={(e) => setPriority(e.target.value)}
                className="w-full bg-console-bg border border-console-border rounded px-2 py-1"
              >
                {PRIORITIES.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
            </div>
          </div>
          <textarea
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="Operator notes (optional)"
            rows={2}
            className="w-full bg-console-bg border border-console-border rounded px-2 py-1 outline-none focus:border-console-accent resize-y"
          />
          <label className="flex items-center gap-2 text-console-muted">
            <input type="checkbox" checked={activate} onChange={(e) => setActivate(e.target.checked)} />
            Activate immediately
          </label>
          <button
            type="button"
            onClick={() => void createIncident()}
            disabled={loading || !settings?.incidentManagementEnabled || templates.length === 0 || !templateId}
            className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
          >
            OPEN INCIDENT
          </button>
        </div>
      )}

      {isAdmin && settingsDraft && (
        <details className="border border-console-border rounded p-2 text-xs">
          <summary className="console-label text-[10px] cursor-pointer">INCIDENT SETTINGS (admin)</summary>
          <div className="mt-2 flex flex-col gap-2">
            <label className="flex items-center gap-2 text-console-muted">
              <input
                type="checkbox"
                checked={settingsDraft.incidentManagementEnabled}
                onChange={(e) => setSettingsDraft({ ...settingsDraft, incidentManagementEnabled: e.target.checked })}
              />
              Incident management enabled
            </label>
            <label className="flex items-center gap-2 text-console-muted">
              <input
                type="checkbox"
                checked={settingsDraft.incidentAutoSuggest}
                onChange={(e) => setSettingsDraft({ ...settingsDraft, incidentAutoSuggest: e.target.checked })}
              />
              Auto-suggest from NWS/IEM (draft signals)
            </label>
            <label className="flex items-center gap-2 text-console-muted">
              <input
                type="checkbox"
                checked={settingsDraft.incidentAutoOpen}
                onChange={(e) => setSettingsDraft({ ...settingsDraft, incidentAutoOpen: e.target.checked })}
              />
              Auto-open tornado warnings
            </label>
            <div>
              <p className="text-console-muted mb-1">Watch areas (state codes)</p>
              <input
                value={(settingsDraft.incidentWatchAreas ?? []).join(', ')}
                onChange={(e) =>
                  setSettingsDraft({
                    ...settingsDraft,
                    incidentWatchAreas: e.target.value.split(',').map((s) => s.trim().toUpperCase()).filter(Boolean),
                  })
                }
                className="w-full bg-console-bg border border-console-border rounded px-2 py-1 outline-none focus:border-console-accent"
                placeholder="OK"
              />
            </div>
            <div>
              <p className="text-console-muted mb-1">Handler hub (optional)</p>
              <select
                value={settingsDraft.incidentHandlerHubId ?? ''}
                onChange={(e) => setSettingsDraft({ ...settingsDraft, incidentHandlerHubId: e.target.value })}
                className="w-full bg-console-bg border border-console-border rounded px-2 py-1"
              >
                <option value="">Standalone (no handler)</option>
                {hubPeers.filter((p) => p.status === 'connected').map((p) => (
                  <option key={p.id} value={p.hubId}>{p.name || p.hubId}</option>
                ))}
              </select>
            </div>
            <button
              type="button"
              onClick={() => void saveSettings()}
              disabled={loading}
              className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
            >
              SAVE SETTINGS
            </button>
          </div>
        </details>
      )}
    </div>
  )
}
