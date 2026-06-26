import { useMemo, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { api, type AuthUser, type RadioSet, type RadioSetSelectionMode, type TalkgroupInfo } from '../../lib/api'
import { PTTButton } from './PTTButton'

type RadioSetsViewProps = Readonly<{
  authUser: AuthUser | null
  audioDevices: MediaDeviceInfo[]
  audioRef: RefObject<HTMLAudioElement | null>
  allGroups: string[]
  distinctTalkgroups: TalkgroupInfo[]
  enumerateAudioDevices: () => void
  radioSets: RadioSet[]
  rsCreateTGs: number[]
  rsCreateMode: RadioSetSelectionMode
  rsCreateGroups: string[]
  rsEditID: string | null
  rsEditName: string
  rsEditTGs: number[]
  rsEditMode: RadioSetSelectionMode
  rsEditGroups: string[]
  rsError: string
  rsLoading: boolean
  rsName: string
  rsPlayingID: string | null
  rsTGSearch: string
  rsGroupSearch: string
  rsVolume: number
  selectedDeviceId: string
  selectedSetID: string
  setRadioSets: Dispatch<SetStateAction<RadioSet[]>>
  setRsCreateTGs: Dispatch<SetStateAction<number[]>>
  setRsCreateMode: Dispatch<SetStateAction<RadioSetSelectionMode>>
  setRsCreateGroups: Dispatch<SetStateAction<string[]>>
  setRsEditID: Dispatch<SetStateAction<string | null>>
  setRsEditName: Dispatch<SetStateAction<string>>
  setRsEditTGs: Dispatch<SetStateAction<number[]>>
  setRsEditMode: Dispatch<SetStateAction<RadioSetSelectionMode>>
  setRsEditGroups: Dispatch<SetStateAction<string[]>>
  setRsError: Dispatch<SetStateAction<string>>
  setRsLoading: Dispatch<SetStateAction<boolean>>
  setRsName: Dispatch<SetStateAction<string>>
  setRsPlayingID: Dispatch<SetStateAction<string | null>>
  setRsTGSearch: Dispatch<SetStateAction<string>>
  setRsGroupSearch: Dispatch<SetStateAction<string>>
  setRsVolume: Dispatch<SetStateAction<number>>
  setSelectedDeviceId: Dispatch<SetStateAction<string>>
  setSelectedSetID: Dispatch<SetStateAction<string>>
  onOpenPTTMode: (radioSetId: string) => void
  onOpenDispatcher: () => void
  onNotify?: (message: string) => void
}>

function isOpenIncidentRadioSet(radioSet: RadioSet): boolean {
  if (radioSet.incidentId) {
    const status = radioSet.incidentStatus ?? 'active'
    return status === 'active' || status === 'draft' || status === 'monitoring'
  }
  return radioSet.name.startsWith('INC ·')
}

function pluralTalkgroups(count: number): string {
  return count === 1 ? 'talkgroup' : 'talkgroups'
}

function replaceRadioSet(radioSets: RadioSet[], radioSet: RadioSet): RadioSet[] {
  return radioSets.map((row) => row.id === radioSet.id ? radioSet : row)
}

function copyToClipboard(text: string) {
  void navigator.clipboard?.writeText(text)
}

function publicMetaWSURL(token: string): string {
  const protocol = globalThis.window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${globalThis.window.location.host}/public/ws-meta/${encodeURIComponent(token)}`
}

function FieldUnitConfigRow({
  label,
  hint,
  value,
}: Readonly<{ label: string; hint: string; value: string }>) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
      <div className="flex flex-col min-w-0 sm:w-28 flex-shrink-0">
        <span className="text-[10px] text-console-muted">{label}</span>
        <span className="text-[9px] text-console-muted/80 uppercase tracking-wider">{hint}</span>
      </div>
      <input
        readOnly
        value={value}
        onClick={(event) => (event.target as HTMLInputElement).select()}
        className="flex-1 bg-console-bg border border-console-border/50 rounded px-1.5 py-0.5 text-[10px] text-console-accent font-mono outline-none min-w-0"
      />
      <button
        type="button"
        onClick={() => copyToClipboard(value)}
        className="px-1.5 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent flex-shrink-0"
      >
        COPY
      </button>
    </div>
  )
}

function radioSetMembershipLabel(radioSet: RadioSet): string {
  if (radioSet.selectionMode === 'groups') {
    const count = radioSet.talkgroupGroups?.length ?? 0
    return `${count} ${count === 1 ? 'group' : 'groups'}`
  }
  return `${radioSet.talkgroups.length} ${pluralTalkgroups(radioSet.talkgroups.length)}`
}

export function RadioSetsView({
  authUser,
  audioDevices,
  audioRef,
  allGroups,
  distinctTalkgroups,
  enumerateAudioDevices,
  radioSets,
  rsCreateTGs,
  rsCreateMode,
  rsCreateGroups,
  rsEditID,
  rsEditName,
  rsEditTGs,
  rsEditMode,
  rsEditGroups,
  rsError,
  rsLoading,
  rsName,
  rsPlayingID,
  rsTGSearch,
  rsGroupSearch,
  rsVolume,
  selectedDeviceId,
  selectedSetID,
  setRadioSets,
  setRsCreateTGs,
  setRsCreateMode,
  setRsCreateGroups,
  setRsEditID,
  setRsEditName,
  setRsEditTGs,
  setRsEditMode,
  setRsEditGroups,
  setRsError,
  setRsLoading,
  setRsName,
  setRsPlayingID,
  setRsTGSearch,
  setRsGroupSearch,
  setRsVolume,
  setSelectedDeviceId,
  setSelectedSetID,
  onOpenPTTMode,
  onOpenDispatcher,
  onNotify,
}: RadioSetsViewProps) {
  const filteredTalkgroups = useMemo(() => {
    const query = rsTGSearch.trim().toLowerCase()
    if (!query) return distinctTalkgroups
    return distinctTalkgroups.filter((tg) =>
      [String(tg.talkgroup), tg.talkgroupLabel, tg.talkgroupGroup]
        .join(' ')
        .toLowerCase()
        .includes(query),
    )
  }, [distinctTalkgroups, rsTGSearch])

  const filteredGroups = useMemo(() => {
    const query = rsGroupSearch.trim().toLowerCase()
    if (!query) return allGroups
    return allGroups.filter((group) => group.toLowerCase().includes(query))
  }, [allGroups, rsGroupSearch])

  const resetEditForm = () => {
    setRsEditID(null)
    setRsEditName('')
    setRsEditTGs([])
    setRsEditGroups([])
    setRsEditMode('talkgroups')
  }

  const refreshRadioSets = async () => {
    try {
      const sets = await api.radioSets()
      setRadioSets(sets)
    } catch {
      setRsError('Could not refresh radio sets')
    }
  }

  const endIncidentForRadioSet = async (radioSet: RadioSet) => {
    const label = radioSet.incidentTitle ?? radioSet.name
    if (
      !globalThis.confirm(
        `End incident "${label}"?\n\nCloses the incident, stops Discord rooms, and revokes the public player link.`,
      )
    ) {
      return
    }
    setRsLoading(true)
    setRsError('')
    try {
      await api.closeIncidentByRadioSet(radioSet.id)
      onNotify?.('Incident ended')
      await refreshRadioSets()
    } catch {
      setRsError('Could not end incident — are you admin/dispatcher with incident management enabled?')
      onNotify?.('End incident failed')
    } finally {
      setRsLoading(false)
    }
  }

  const submitRadioSet = async () => {
    const name = (rsEditID ? rsEditName : rsName).trim()
    if (!name) {
      setRsError('Name is required')
      return
    }

    setRsLoading(true)
    setRsError('')
    try {
      if (rsEditID) {
        const updated = await api.updateRadioSet(rsEditID, name, rsEditMode, rsEditTGs, rsEditGroups)
        setRadioSets((prev) => replaceRadioSet(prev, updated))
        resetEditForm()
        return
      }

      const created = await api.createRadioSet(name, rsCreateMode, rsCreateTGs, rsCreateGroups)
      setRadioSets((prev) => [...prev, created])
      setRsName('')
      setRsCreateTGs([])
      setRsCreateGroups([])
      setRsCreateMode('talkgroups')
    } catch {
      setRsError(rsEditID ? 'Could not update radio set' : 'Could not create radio set')
    } finally {
      setRsLoading(false)
    }
  }

  const toggleRadioSetScanning = (radioSetID: string) => {
    setRsPlayingID((prev) => {
      const next = prev === radioSetID ? null : radioSetID
      if (next) enumerateAudioDevices()
      return next
    })
  }

  const selectAudioDevice = (deviceId: string) => {
    setSelectedDeviceId(deviceId)
    if (audioRef.current && 'setSinkId' in audioRef.current) {
      (audioRef.current as HTMLAudioElement & { setSinkId(id: string): Promise<void> })
        .setSinkId(deviceId)
        .catch(console.error)
    }
  }

  const updateRadioSetVolume = (volume: number) => {
    const nextVolume = Math.min(100, Math.max(1, Math.round(volume)))
    setRsVolume(nextVolume)
    if (audioRef.current) {
      audioRef.current.volume = nextVolume / 100
    }
  }

  const generateShareLink = async (radioSetID: string) => {
    try {
      const updated = await api.generateShareToken(radioSetID)
      setRadioSets((prev) => replaceRadioSet(prev, updated))
    } catch {
      setRsError('Could not generate share link')
    }
  }

  const deleteRadioSet = async (radioSet: RadioSet) => {
    if (!globalThis.window.confirm(`Delete radio set "${radioSet.name}"?`)) return
    try {
      await api.deleteRadioSet(radioSet.id)
      setRadioSets((prev) => prev.filter((row) => row.id !== radioSet.id))
      if (selectedSetID === radioSet.id) setSelectedSetID('')
      if (rsPlayingID === radioSet.id) setRsPlayingID(null)
    } catch {
      setRsError('Could not delete radio set')
    }
  }

  const revokeShareLink = async (radioSetID: string) => {
    try {
      const updated = await api.revokeShareToken(radioSetID)
      setRadioSets((prev) => replaceRadioSet(prev, updated))
    } catch {
      setRsError('Could not revoke share link')
    }
  }

  return (
    <main className="console-panel flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <span className="console-label text-xs">RADIO SETS</span>
        <div className="flex items-center gap-2">
          {authUser?.txEnabled && authUser?.dispatcherEnabled && (
            <button
              onClick={onOpenDispatcher}
              className="px-2 py-1 sm:py-0.5 border border-console-amber text-console-amber rounded text-[10px] uppercase tracking-wider hover:bg-console-amber/10"
              title="Broadcast a single PTT to multiple radio sets at once"
            >
              DISPATCHER
            </button>
          )}
          <span className="text-xs text-console-muted tabular-nums">{radioSets.length} sets</span>
        </div>
      </div>

      {rsError && (
        <p className="text-xs text-console-error border border-console-error rounded px-2 py-1">{rsError}</p>
      )}

      {!rsEditID && (
        <div className="border border-console-border rounded p-3 flex flex-col gap-3">
          <p className="console-label text-xs">CREATE RADIO SET</p>
          <input
            value={rsName}
            onChange={(event) => setRsName(event.target.value)}
            placeholder="Set name..."
            className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
          />
          <div className="flex gap-2 flex-wrap">
            <button
              type="button"
              onClick={() => setRsCreateMode('talkgroups')}
              className={`px-2 py-1 border rounded text-[10px] uppercase tracking-wider ${
                rsCreateMode === 'talkgroups'
                  ? 'border-console-accent text-console-accent bg-console-accent/10'
                  : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
              }`}
            >
              Talkgroups
            </button>
            <button
              type="button"
              onClick={() => setRsCreateMode('groups')}
              className={`px-2 py-1 border rounded text-[10px] uppercase tracking-wider ${
                rsCreateMode === 'groups'
                  ? 'border-console-accent text-console-accent bg-console-accent/10'
                  : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
              }`}
            >
              Groups
            </button>
          </div>
          {rsCreateMode === 'talkgroups' ? (
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <span className="text-[10px] text-console-muted uppercase tracking-wider">Talkgroups</span>
                <input
                  value={rsTGSearch}
                  onChange={(event) => setRsTGSearch(event.target.value)}
                  placeholder="Filter..."
                  className="bg-console-bg border border-console-border rounded px-2 py-0.5 text-[10px] outline-none focus:border-console-accent w-full sm:w-32"
                />
              </div>
              <div className="max-h-48 overflow-y-auto border border-console-border rounded divide-y divide-console-border/50">
                {filteredTalkgroups.map((tg) => {
                  const checked = rsCreateTGs.includes(tg.talkgroup)
                  return (
                    <label key={tg.talkgroup} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => setRsCreateTGs(
                          checked
                            ? rsCreateTGs.filter((t) => t !== tg.talkgroup)
                            : [...rsCreateTGs, tg.talkgroup],
                        )}
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
          ) : (
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between gap-2 flex-wrap">
                <span className="text-[10px] text-console-muted uppercase tracking-wider">Talkgroup groups</span>
                <input
                  value={rsGroupSearch}
                  onChange={(event) => setRsGroupSearch(event.target.value)}
                  placeholder="Filter..."
                  className="bg-console-bg border border-console-border rounded px-2 py-0.5 text-[10px] outline-none focus:border-console-accent w-full sm:w-32"
                />
              </div>
              <p className="text-[10px] text-console-muted">
                Dynamic playset — new talkgroups in these groups are included automatically.
              </p>
              <div className="max-h-48 overflow-y-auto border border-console-border rounded divide-y divide-console-border/50">
                {filteredGroups.map((group) => {
                  const checked = rsCreateGroups.includes(group)
                  return (
                    <label key={group} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => setRsCreateGroups(
                          checked
                            ? rsCreateGroups.filter((g) => g !== group)
                            : [...rsCreateGroups, group],
                        )}
                      />
                      <span>{group}</span>
                    </label>
                  )
                })}
                {allGroups.length === 0 && (
                  <p className="text-[10px] text-console-muted px-2 py-2">No groups seen yet</p>
                )}
              </div>
            </div>
          )}
          <div className="flex gap-2 flex-wrap">
            <button
              onClick={submitRadioSet}
              disabled={rsLoading}
              className="px-3 py-1 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50 flex-1 sm:flex-none"
            >
              {rsLoading ? 'SAVING...' : 'CREATE'}
            </button>
          </div>
        </div>
      )}

      {radioSets.length === 0 ? (
        <p className="text-xs text-console-muted">No radio sets created yet</p>
      ) : (
        <div className="flex flex-col gap-2">
          {radioSets.map((radioSet) => {
            const canManageRadioSet =
              radioSet.userId === authUser?.id || authUser?.role === 'admin'
            const canEndIncident =
              (authUser?.role === 'admin' || authUser?.dispatcherEnabled) && isOpenIncidentRadioSet(radioSet)
            const isScanning = rsPlayingID === radioSet.id
            return (
              <div
                key={radioSet.id}
                className={`border rounded p-2.5 flex flex-col gap-2 ${
                  canEndIncident ? 'border-console-accent/50 bg-console-accent/5' : 'border-console-border'
                }`}
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="text-xs font-semibold">{radioSet.name}</span>
                    {canEndIncident && (
                      <span className="text-[10px] text-console-accent uppercase tracking-wider">
                        open incident · {radioSet.incidentStatus ?? 'active'}
                      </span>
                    )}
                    <span className="text-[10px] text-console-muted">{radioSetMembershipLabel(radioSet)}</span>
                    {authUser?.role === 'admin' && (
                      <span className="text-[10px] text-console-muted">
                        owner {radioSet.userId || 'unknown'} | sources {(radioSet.sourceIds && radioSet.sourceIds.length > 0) ? radioSet.sourceIds.join(', ') : 'none'}
                      </span>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-shrink-0 sm:flex-wrap sm:justify-end">
                    <button
                      onClick={() => toggleRadioSetScanning(radioSet.id)}
                      className={`px-2 py-1 sm:py-0.5 border rounded text-[10px] ${
                        isScanning
                          ? 'border-console-accent text-console-accent bg-console-accent/10'
                          : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
                      }`}
                    >
                      {isScanning ? '■ SCANNING' : '▶ SCAN'}
                    </button>
                    {isScanning && audioDevices.length > 0 && (
                      <select
                        value={selectedDeviceId}
                        onChange={(event) => selectAudioDevice(event.target.value)}
                        className="col-span-2 bg-console-bg border border-console-border rounded px-2 py-1 sm:py-0.5 text-[10px] text-console-muted outline-none focus:border-console-accent min-w-0"
                        title="Audio output device"
                      >
                        <option value="">Output: Default</option>
                        {audioDevices.map((device) => (
                          <option key={device.deviceId} value={device.deviceId}>
                            {device.label || `Device ${device.deviceId.slice(0, 6)}`}
                          </option>
                        ))}
                      </select>
                    )}
                    {isScanning && (
                      <label className="col-span-2 flex items-center gap-2 bg-console-bg border border-console-border rounded px-2 py-1 sm:py-0.5 text-[10px] text-console-muted">
                        <span className="uppercase tracking-wider flex-shrink-0">Vol</span>
                        <input
                          type="range"
                          min="1"
                          max="100"
                          step="1"
                          value={rsVolume}
                          onChange={(event) => updateRadioSetVolume(Number(event.target.value))}
                          className="min-w-0 flex-1 accent-console-accent"
                          title="Radio set volume"
                        />
                        <span className="w-8 text-right tabular-nums text-console-accent">{rsVolume}%</span>
                      </label>
                    )}
                    {authUser?.txEnabled && radioSet.pttTalkgroup !== undefined && (
                      <>
                        <PTTButton radioSetId={radioSet.id} />
                        <button
                          onClick={() => onOpenPTTMode(radioSet.id)}
                          className="px-2 py-1 sm:py-0.5 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                          title="Open dedicated PTT mode with spacebar binding"
                        >
                          KEY
                        </button>
                      </>
                    )}
                    {canEndIncident && (
                      <button
                        type="button"
                        onClick={() => void endIncidentForRadioSet(radioSet)}
                        disabled={rsLoading}
                        className="col-span-2 px-2 py-1 sm:py-0.5 border border-console-error text-console-error rounded text-[10px] font-semibold hover:bg-console-error/10 disabled:opacity-50"
                      >
                        END INCIDENT
                      </button>
                    )}
                    <button
                      onClick={() => generateShareLink(radioSet.id)}
                      className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                      disabled={!canManageRadioSet}
                    >
                      SHARE
                    </button>
                    <button
                      onClick={() => {
                        if (!radioSet.shareToken) return
                        globalThis.window.open(`/public/player/${radioSet.shareToken}`, '_blank', 'noopener,noreferrer')
                      }}
                      className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-30 disabled:cursor-not-allowed"
                      disabled={!radioSet.shareToken}
                      title={radioSet.shareToken ? 'Open public player' : 'Generate a share link first'}
                    >
                      OPEN
                    </button>
                    <button
                      onClick={() => {
                        setRsEditID(radioSet.id)
                        setRsEditName(radioSet.name)
                        setRsEditMode(radioSet.selectionMode ?? 'talkgroups')
                        setRsEditTGs([...radioSet.talkgroups])
                        setRsEditGroups([...(radioSet.talkgroupGroups ?? [])])
                      }}
                      className="px-2 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                      disabled={!canManageRadioSet}
                    >
                      EDIT
                    </button>
                    <button
                      onClick={() => deleteRadioSet(radioSet)}
                      className="px-2 py-1 sm:py-0.5 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                      disabled={!canManageRadioSet}
                    >
                      DEL
                    </button>
                  </div>
                </div>

                <div className="border border-console-border/50 rounded p-2 flex flex-col gap-2 bg-console-bg/40">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-[10px] text-console-muted uppercase tracking-wider">Field unit</span>
                    <span className="text-[9px] text-console-muted">
                      Copy into handheld hub_config.h. PTT login stays off the device — USB serial once per handheld account.
                    </span>
                  </div>
                  <FieldUnitConfigRow label="Set ID" hint="HUB_RADIO_SET_ID" value={radioSet.id} />
                  {radioSet.shareToken ? (
                    <FieldUnitConfigRow label="Listen token" hint="HUB_SHARE_TOKEN" value={radioSet.shareToken} />
                  ) : (
                    <p className="text-[10px] text-console-muted">
                      No listen token yet — click <span className="text-console-text">SHARE</span> above, then copy it here.
                    </p>
                  )}
                  <p className="text-[9px] text-console-muted border-t border-console-border/40 pt-1.5">
                    PTT: create a dedicated hub user with TX enabled. On the device USB serial run{' '}
                    <span className="font-mono text-console-text">login email password</span> once (session cached ~24h).
                    Do not put hub passwords in firmware source.
                  </p>
                </div>

                {radioSet.shareToken && (
                  <div className="border border-console-border/50 rounded p-2 flex flex-col gap-1.5 bg-console-bg/40">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <span className="text-[10px] text-console-muted uppercase tracking-wider">Share links</span>
                      <button
                        onClick={() => revokeShareLink(radioSet.id)}
                        className="px-1.5 py-0.5 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                        disabled={!canManageRadioSet}
                      >
                        REVOKE
                      </button>
                    </div>
                    {[
                      {
                        label: 'Player',
                        value: `${globalThis.window.location.origin}/public/player/${radioSet.shareToken}`,
                      },
                      {
                        label: 'Meta WS',
                        value: publicMetaWSURL(radioSet.shareToken),
                      },
                    ].map(({ label, value }) => (
                      <div key={label} className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
                        <span className="text-[10px] text-console-muted w-14 flex-shrink-0">{label}</span>
                        <input
                          readOnly
                          value={value}
                          onClick={(event) => (event.target as HTMLInputElement).select()}
                          className="flex-1 bg-console-bg border border-console-border/50 rounded px-1.5 py-0.5 text-[10px] text-console-accent outline-none min-w-0"
                        />
                        <button
                          onClick={() => copyToClipboard(value)}
                          className="px-1.5 py-1 sm:py-0.5 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent flex-shrink-0"
                        >
                          COPY
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                {rsEditID === radioSet.id && (
                  <div className="border border-console-accent/30 rounded p-3 flex flex-col gap-3 bg-console-bg/40">
                    <p className="console-label text-[10px]">EDIT RADIO SET</p>
                    <input
                      value={rsEditName}
                      onChange={(event) => setRsEditName(event.target.value)}
                      placeholder="Set name..."
                      className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
                    />
                    <div className="flex gap-2 flex-wrap">
                      <button
                        type="button"
                        onClick={() => setRsEditMode('talkgroups')}
                        className={`px-2 py-1 border rounded text-[10px] uppercase tracking-wider ${
                          rsEditMode === 'talkgroups'
                            ? 'border-console-accent text-console-accent bg-console-accent/10'
                            : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
                        }`}
                      >
                        Talkgroups
                      </button>
                      <button
                        type="button"
                        onClick={() => setRsEditMode('groups')}
                        className={`px-2 py-1 border rounded text-[10px] uppercase tracking-wider ${
                          rsEditMode === 'groups'
                            ? 'border-console-accent text-console-accent bg-console-accent/10'
                            : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
                        }`}
                      >
                        Groups
                      </button>
                    </div>
                    {rsEditMode === 'talkgroups' ? (
                      <div className="flex flex-col gap-1">
                        <div className="flex items-center justify-between gap-2 flex-wrap">
                          <span className="text-[10px] text-console-muted uppercase tracking-wider">Talkgroups</span>
                          <input
                            value={rsTGSearch}
                            onChange={(event) => setRsTGSearch(event.target.value)}
                            placeholder="Filter..."
                            className="bg-console-bg border border-console-border rounded px-2 py-0.5 text-[10px] outline-none focus:border-console-accent w-full sm:w-32"
                          />
                        </div>
                        <div className="max-h-48 overflow-y-auto border border-console-border rounded divide-y divide-console-border/50">
                          {filteredTalkgroups.map((tg) => {
                            const checked = rsEditTGs.includes(tg.talkgroup)
                            return (
                              <label key={tg.talkgroup} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                                <input
                                  type="checkbox"
                                  checked={checked}
                                  onChange={() => setRsEditTGs(
                                    checked
                                      ? rsEditTGs.filter((t) => t !== tg.talkgroup)
                                      : [...rsEditTGs, tg.talkgroup],
                                  )}
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
                    ) : (
                      <div className="flex flex-col gap-1">
                        <div className="flex items-center justify-between gap-2 flex-wrap">
                          <span className="text-[10px] text-console-muted uppercase tracking-wider">Talkgroup groups</span>
                          <input
                            value={rsGroupSearch}
                            onChange={(event) => setRsGroupSearch(event.target.value)}
                            placeholder="Filter..."
                            className="bg-console-bg border border-console-border rounded px-2 py-0.5 text-[10px] outline-none focus:border-console-accent w-full sm:w-32"
                          />
                        </div>
                        <p className="text-[10px] text-console-muted">
                          Dynamic playset — new talkgroups in these groups are included automatically.
                        </p>
                        <div className="max-h-48 overflow-y-auto border border-console-border rounded divide-y divide-console-border/50">
                          {filteredGroups.map((group) => {
                            const checked = rsEditGroups.includes(group)
                            return (
                              <label key={group} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                                <input
                                  type="checkbox"
                                  checked={checked}
                                  onChange={() => setRsEditGroups(
                                    checked
                                      ? rsEditGroups.filter((g) => g !== group)
                                      : [...rsEditGroups, group],
                                  )}
                                />
                                <span>{group}</span>
                              </label>
                            )
                          })}
                          {allGroups.length === 0 && (
                            <p className="text-[10px] text-console-muted px-2 py-2">No groups seen yet</p>
                          )}
                        </div>
                      </div>
                    )}
                    <div className="flex gap-2 flex-wrap">
                      <button
                        onClick={submitRadioSet}
                        disabled={rsLoading}
                        className="px-3 py-1 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50 flex-1 sm:flex-none"
                      >
                        {rsLoading ? 'SAVING...' : 'SAVE'}
                      </button>
                      <button
                        onClick={resetEditForm}
                        className="px-3 py-1 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-widest hover:border-console-accent hover:text-console-accent flex-1 sm:flex-none"
                      >
                        CANCEL
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </main>
  )
}