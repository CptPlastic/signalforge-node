import { useMemo, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { api, type AuthUser, type RadioSet, type TalkgroupInfo } from '../../lib/api'
import { PTTButton } from './PTTButton'

type RadioSetsViewProps = Readonly<{
  authUser: AuthUser | null
  audioDevices: MediaDeviceInfo[]
  audioRef: RefObject<HTMLAudioElement | null>
  distinctTalkgroups: TalkgroupInfo[]
  enumerateAudioDevices: () => void
  radioSets: RadioSet[]
  rsCreateTGs: number[]
  rsEditID: string | null
  rsEditName: string
  rsEditTGs: number[]
  rsError: string
  rsLoading: boolean
  rsName: string
  rsPlayingID: string | null
  rsTGSearch: string
  rsVolume: number
  selectedDeviceId: string
  selectedSetID: string
  setRadioSets: Dispatch<SetStateAction<RadioSet[]>>
  setRsCreateTGs: Dispatch<SetStateAction<number[]>>
  setRsEditID: Dispatch<SetStateAction<string | null>>
  setRsEditName: Dispatch<SetStateAction<string>>
  setRsEditTGs: Dispatch<SetStateAction<number[]>>
  setRsError: Dispatch<SetStateAction<string>>
  setRsLoading: Dispatch<SetStateAction<boolean>>
  setRsName: Dispatch<SetStateAction<string>>
  setRsPlayingID: Dispatch<SetStateAction<string | null>>
  setRsTGSearch: Dispatch<SetStateAction<string>>
  setRsVolume: Dispatch<SetStateAction<number>>
  setSelectedDeviceId: Dispatch<SetStateAction<string>>
  setSelectedSetID: Dispatch<SetStateAction<string>>
}>

function radioSetSubmitLabel(rsLoading: boolean, rsEditID: string | null): string {
  if (rsLoading) return 'SAVING...'
  if (rsEditID) return 'SAVE'
  return 'CREATE'
}

function pluralTalkgroups(count: number): string {
  return count === 1 ? 'talkgroup' : 'talkgroups'
}

function replaceRadioSet(radioSets: RadioSet[], radioSet: RadioSet): RadioSet[] {
  return radioSets.map((row) => row.id === radioSet.id ? radioSet : row)
}

function updateTalkgroupSelection(talkgroup: number, checked: boolean) {
  return (talkgroups: number[]): number[] => {
    if (checked) return talkgroups.filter((id) => id !== talkgroup)
    return [...talkgroups, talkgroup]
  }
}

export function RadioSetsView({
  authUser,
  audioDevices,
  audioRef,
  distinctTalkgroups,
  enumerateAudioDevices,
  radioSets,
  rsCreateTGs,
  rsEditID,
  rsEditName,
  rsEditTGs,
  rsError,
  rsLoading,
  rsName,
  rsPlayingID,
  rsTGSearch,
  rsVolume,
  selectedDeviceId,
  selectedSetID,
  setRadioSets,
  setRsCreateTGs,
  setRsEditID,
  setRsEditName,
  setRsEditTGs,
  setRsError,
  setRsLoading,
  setRsName,
  setRsPlayingID,
  setRsTGSearch,
  setRsVolume,
  setSelectedDeviceId,
  setSelectedSetID,
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

  const selectedTalkgroups = rsEditID ? rsEditTGs : rsCreateTGs
  const setSelectedTalkgroups = rsEditID ? setRsEditTGs : setRsCreateTGs

  const resetEditForm = () => {
    setRsEditID(null)
    setRsEditName('')
    setRsEditTGs([])
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
        const updated = await api.updateRadioSet(rsEditID, name, rsEditTGs)
        setRadioSets((prev) => replaceRadioSet(prev, updated))
        resetEditForm()
        return
      }

      const created = await api.createRadioSet(name, rsCreateTGs)
      setRadioSets((prev) => [...prev, created])
      setRsName('')
      setRsCreateTGs([])
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
          onChange={(event) => rsEditID ? setRsEditName(event.target.value) : setRsName(event.target.value)}
          placeholder="Set name..."
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
        />
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
              const checked = selectedTalkgroups.includes(tg.talkgroup)
              return (
                <label key={tg.talkgroup} className="flex items-center gap-2 px-2 py-1 cursor-pointer hover:bg-console-surface text-xs">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => setSelectedTalkgroups(updateTalkgroupSelection(tg.talkgroup, checked))}
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
            onClick={submitRadioSet}
            disabled={rsLoading}
            className="px-3 py-1 border border-console-accent text-console-accent rounded text-[10px] uppercase tracking-widest hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50 flex-1 sm:flex-none"
          >
            {radioSetSubmitLabel(rsLoading, rsEditID)}
          </button>
          {rsEditID && (
            <button
              onClick={resetEditForm}
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
          {radioSets.map((radioSet) => {
            const canManageRadioSet = radioSet.userId === authUser?.id
            const isScanning = rsPlayingID === radioSet.id
            return (
              <div key={radioSet.id} className="border border-console-border rounded p-2.5 flex flex-col gap-2">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="text-xs font-semibold">{radioSet.name}</span>
                    <span className="text-[10px] text-console-muted">{radioSet.talkgroups.length} {pluralTalkgroups(radioSet.talkgroups.length)}</span>
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
                      <PTTButton radioSetId={radioSet.id} />
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
                        setRsEditTGs([...radioSet.talkgroups])
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
                      { label: 'Player', url: `/public/player/${radioSet.shareToken}` },
                    ].map(({ label, url }) => (
                      <div key={label} className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
                        <span className="text-[10px] text-console-muted w-10 flex-shrink-0">{label}</span>
                        <input
                          readOnly
                          value={`${globalThis.window.location.origin}${url}`}
                          onClick={(event) => (event.target as HTMLInputElement).select()}
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
  )
}