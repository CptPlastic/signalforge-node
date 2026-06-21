import { useCallback, useEffect, useState } from 'react'
import {
  api,
  type AuthUser,
  type HubPeer,
  type Incident,
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
}>

export function IncidentsPanel({ authUser, isAdmin, hubPeers, onNotify }: Props) {
  const canManage = isAdmin || !!authUser.dispatcherEnabled

  const [settings, setSettings] = useState<IncidentSettings | null>(null)
  const [settingsDraft, setSettingsDraft] = useState<IncidentSettings | null>(null)
  const [templates, setTemplates] = useState<IncidentTemplate[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [signals, setSignals] = useState<IncidentSignal[]>([])
  const [loading, setLoading] = useState(false)
  const [templateId, setTemplateId] = useState('weather-severe')
  const [title, setTitle] = useState('')
  const [activate, setActivate] = useState(true)

  const refresh = useCallback(async () => {
    if (!canManage) return
    setLoading(true)
    try {
      const [s, t, i, sig] = await Promise.all([
        api.incidentSettings(),
        api.incidentTemplates(),
        api.incidents(),
        api.incidentSignals().catch(() => [] as IncidentSignal[]),
      ])
      setSettings(s)
      setSettingsDraft(s)
      setTemplates(t)
      setIncidents(i)
      setSignals(sig)
    } catch (err) {
      console.error(err)
      onNotify('Could not load incidents')
    } finally {
      setLoading(false)
    }
  }, [canManage, onNotify])

  useEffect(() => {
    void refresh()
  }, [refresh])

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
        activate,
      })
      onNotify(resp.shareUrl ? `Incident active — ${resp.shareUrl}` : 'Incident created')
      setTitle('')
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

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <p className="console-label text-xs">// INCIDENTS</p>
          <p className="text-[11px] text-console-muted">NWS/IEM signals → radio sets on the fly.</p>
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
            Auto-suggest from NWS/IEM (draft incidents)
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
            <p className="text-console-muted mb-1">Watch areas (state codes, comma-separated)</p>
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
            <p className="text-console-muted mb-1">Handler hub (optional peer)</p>
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
            <p className="text-console-error text-[11px]">Enable incident management and save to activate poller + create.</p>
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
          {templates.map((t) => (
            <option key={t.id} value={t.id}>{t.name}</option>
          ))}
        </select>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Severe Weather — Yukon"
          className="w-full bg-console-bg border border-console-border rounded px-2 py-1 outline-none focus:border-console-accent"
        />
        <label className="flex items-center gap-2 text-console-muted">
          <input type="checkbox" checked={activate} onChange={(e) => setActivate(e.target.checked)} />
          Activate immediately (+ share link if community template)
        </label>
        <button
          type="button"
          onClick={() => void createIncident()}
          disabled={loading || !settings?.incidentManagementEnabled}
          className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
        >
          OPEN INCIDENT
        </button>
      </div>

      {signals.length > 0 && (
        <div className="flex flex-col gap-1 text-xs">
          <div className="flex items-center justify-between">
            <p className="console-label text-[10px]">SUGGESTED SIGNALS</p>
            <button type="button" onClick={() => void pollSignals()} disabled={loading} className="text-[10px] text-console-muted hover:text-console-accent">
              POLL NWS/IEM
            </button>
          </div>
          {signals.slice(0, 8).map((sig) => (
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
                    .then((r) => { onNotify(r.shareUrl || 'Promoted'); return refresh() })
                    .catch(() => onNotify('Promote failed'))
                    .finally(() => setLoading(false))
                }}
                className="shrink-0 px-2 py-0.5 border border-console-accent text-console-accent rounded text-[10px]"
              >
                OPEN
              </button>
            </div>
          ))}
        </div>
      )}

      {incidents.length > 0 && (
        <div className="flex flex-col gap-1 text-xs">
          <p className="console-label text-[10px]">ACTIVE / RECENT</p>
          {incidents.map((inc) => (
            <div key={inc.id} className="border border-console-border rounded p-2 flex justify-between gap-2 items-start">
              <div>
                <div className="text-console-text">{inc.title}</div>
                <div className="text-console-muted text-[10px] uppercase">{inc.status} · {inc.priority} · {inc.exposure}</div>
              </div>
              <div className="flex gap-1 shrink-0">
                {inc.status === 'draft' && (
                  <button type="button" disabled={loading} onClick={() => { setLoading(true); api.activateIncident(inc.id).then(() => refresh()).finally(() => setLoading(false)) }} className="px-2 py-0.5 border border-console-border rounded text-[10px]">ACTIVATE</button>
                )}
                {inc.status !== 'closed' && inc.status !== 'archived' && (
                  <button type="button" disabled={loading} onClick={() => { setLoading(true); api.closeIncident(inc.id).then(() => refresh()).finally(() => setLoading(false)) }} className="px-2 py-0.5 border border-console-error text-console-error rounded text-[10px]">CLOSE</button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
