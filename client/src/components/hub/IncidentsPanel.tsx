import { useCallback, useEffect, useState } from 'react'
import {
  api,
  type AuthUser,
  type HubPeer,
  type Incident,
  type IncidentDiscordIntegration,
  type IncidentSettings,
  type IncidentSignal,
  type IncidentTemplate,
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

  const refresh = useCallback(async () => {
    if (!canManage) return
    setLoading(true)
    try {
      const [s, t, all, sig] = await Promise.all([
        api.incidentSettings().catch((err) => {
          console.error(err)
          return null
        }),
        api.incidentTemplates().catch((err) => {
          console.error(err)
          onNotify('Could not load incident templates')
          return [] as IncidentTemplate[]
        }),
        api.incidents(true).catch((err) => {
          console.error(err)
          return [] as Incident[]
        }),
        api.incidentSignals().catch(() => [] as IncidentSignal[]),
      ])
      if (s) {
        setSettings(s)
        setSettingsDraft(s)
      }
      setTemplates(t)
      setIncidents(all.filter((i) => i.status === 'active' || i.status === 'draft' || i.status === 'monitoring'))
      setArchivedIncidents(all.filter((i) => i.status === 'closed' || i.status === 'archived'))
      setSignals(sig)
      const active = all.filter((i) => i.status === 'active' || i.status === 'monitoring')
      const discordEntries = await Promise.all(
        active.map(async (inc) => {
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
      setTitle('')
      setNotes('')
      await refresh()
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

  function renderIncidentRow(inc: Incident, actions: 'full' | 'archive-only') {
    const isActive = inc.status === 'active' || inc.status === 'draft' || inc.status === 'monitoring'
    const discord = discordByIncident[inc.id]
    const discordLabel = discord?.status === 'active'
      ? 'DISCORD LIVE'
      : discord?.status === 'pending'
        ? 'DISCORD PENDING'
        : discord?.status === 'stopping'
          ? 'DISCORD STOPPING'
          : discord?.status === 'failed'
            ? 'DISCORD FAILED'
            : 'DISCORD ROOMS'
    return (
      <div key={inc.id} className="border border-console-border rounded p-2 flex flex-col gap-2">
        <div className="flex justify-between gap-2 items-start">
          <div>
            <div className="text-console-text">{inc.title}</div>
            <div className="text-console-muted text-[10px] uppercase">
              {inc.status} · {inc.priority} · {inc.exposure}
              {inc.openedAt ? ` · opened ${fmtDateTime(inc.openedAt)}` : ''}
              {discord?.status ? ` · discord ${discord.status}` : ''}
              {discord?.config?.error ? ` · ${discord.config.error}` : ''}
            </div>
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
                className="px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
              >
                ACTIVATE
              </button>
            )}
            {actions === 'full' && isActive && (
              <button
                type="button"
                disabled={loading}
                onClick={() => {
                  setLoading(true)
                  api.closeIncident(inc.id)
                    .then(() => { onNotify('Incident closed'); return refresh() })
                    .catch(() => onNotify('Close failed'))
                    .finally(() => setLoading(false))
                }}
                className="px-2 py-0.5 border border-console-error text-console-error rounded text-[10px]"
              >
                CLOSE
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
                className="px-2 py-0.5 border border-console-border rounded text-[10px]"
              >
                ARCHIVE
              </button>
            )}
          </div>
        </div>
        <div className="flex gap-2 flex-wrap">
          {inc.radioSetId && onOpenRadioSet && isActive && (
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
                      .then(() => { onNotify('Discord rooms + voice stream requested'); return refresh() })
                      .catch(async (err) => {
                        const msg = err instanceof Error ? err.message : 'Discord request failed'
                        onNotify(msg.includes('503') || msg.includes('not configured')
                          ? 'Discord not linked — set DISCORD_BOT_WORKER_TOKEN on api + discord-bot'
                          : msg)
                      })
                      .finally(() => setLoading(false))
                  }}
                  className="px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
                  title="Creates voice + text channels; bot streams live audio to voice"
                >
                  {discordLabel}
                </button>
              )}
            </>
          )}
          {discord?.config?.error && (
            <span className="text-[10px] text-console-error">{discord.config.error}</span>
          )}
        </div>
      </div>
    )
  }

  const visibleIncidents = tab === 'active' ? incidents : archivedIncidents

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <p className="console-label text-xs">// INCIDENTS</p>
          <p className="text-[11px] text-console-muted">Open incident → radio set → Discord rooms auto-queue when bot is linked.</p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          disabled={loading}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent disabled:opacity-50"
        >
          REFRESH
        </button>
      </div>

      {isAdmin && settingsDraft && (
        <div className="border border-console-border rounded p-2 flex flex-col gap-2 text-xs">
          <p className="console-label text-[10px]">SETTINGS (ADMIN)</p>
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
          {!settings?.incidentManagementEnabled && (
            <p className="text-console-error text-[11px]">Enable incident management and save to activate.</p>
          )}
        </div>
      )}

      <div className="border border-console-border rounded p-2 flex flex-col gap-2 text-xs">
        <p className="console-label text-[10px]">OPEN INCIDENT</p>
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
            <p className="text-[10px] text-console-muted mt-0.5">community = public player link</p>
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

      <div className="flex flex-col gap-1 text-xs">
        <div className="flex items-center justify-between gap-2">
          <p className="console-label text-[10px]">WEATHER SIGNALS</p>
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
          <p className="text-console-muted text-[10px]">No pending signals — poll or wait for auto-suggest.</p>
        ) : (
          signals.slice(0, 8).map((sig) => (
            <div key={sig.id} className="border border-console-border rounded p-2 flex justify-between gap-2">
              <div>
                <div className="text-console-text">{sig.title || sig.eventType}</div>
                <div className="text-console-muted text-[10px]">{sig.source} · {sig.severity} · {fmtDateTime(sig.receivedAt)}</div>
              </div>
              <button
                type="button"
                disabled={loading}
                onClick={() => {
                  setLoading(true)
                  api.promoteIncidentSignal(sig.id)
                    .then((r) => {
                      if (r.discordQueued) onNotify('Incident opened — Discord rooms queued')
                      else if (r.discordSkipReason) onNotify(`Discord not queued: ${r.discordSkipReason}`)
                      else if (r.shareUrl) copyShareUrl(r.shareUrl)
                      else onNotify('Incident opened from signal')
                      return refresh()
                    })
                    .catch(() => onNotify('Promote failed'))
                    .finally(() => setLoading(false))
                }}
                className="shrink-0 px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
              >
                OPEN
              </button>
            </div>
          ))
        )}
      </div>

      <div className="flex gap-2 text-[10px]">
        <button
          type="button"
          onClick={() => setTab('active')}
          className={`px-2 py-0.5 border rounded ${tab === 'active' ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
        >
          ACTIVE ({incidents.length})
        </button>
        <button
          type="button"
          onClick={() => setTab('archive')}
          className={`px-2 py-0.5 border rounded ${tab === 'archive' ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
        >
          CLOSED / ARCHIVE ({archivedIncidents.length})
        </button>
      </div>

      {visibleIncidents.length === 0 ? (
        <p className="text-console-muted text-[11px]">No incidents in this view.</p>
      ) : (
        visibleIncidents.map((inc) => renderIncidentRow(inc, tab === 'active' ? 'full' : 'archive-only'))
      )}
    </div>
  )
}
