import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError, type Call, type PTTBroadcastResult, type RadioSet } from '../../lib/api'
import { playChirp } from '../../lib/chirp'
import { volumeToGain } from '../../lib/monitorAudioSettings'
import { pickPttMimeType, pttBlobMimeType } from '../../lib/pttRecording'
import { MonitorAudioBar } from '../MonitorAudioBar'

type Props = Readonly<{
  radioSets: RadioSet[]
  latestCall: Call | null
  rsVolume: number
  setRsVolume: (value: number) => void
  chirpVolume: number
  setChirpVolume: (value: number) => void
  onBack: () => void
}>

// How long a set card stays "lit" (pulsing + showing call info) after a call
// lands on it. Refreshed on each new matching call.
const ACTIVITY_LINGER_MS = 8000

type Activity = {
  call: Call
  expiresAt: number
}

type State = 'idle' | 'recording' | 'uploading' | 'error'

const MIN_DURATION_MS = 300
const MAX_DURATION_MS = 30_000

function newClientId(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `disp-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

export function DispatcherView({
  radioSets,
  latestCall,
  rsVolume,
  setRsVolume,
  chirpVolume,
  setChirpVolume,
  onBack,
}: Props) {
  const eligibleSets = useMemo(
    () => radioSets.filter((rs) => rs.pttTalkgroup !== undefined),
    [radioSets],
  )

  const [selectedIds, setSelectedIds] = useState<Set<string>>(
    () => new Set(eligibleSets.map((rs) => rs.id)),
  )
  const [state, setState] = useState<State>('idle')
  const [error, setError] = useState<string>('')
  const [lastResults, setLastResults] = useState<PTTBroadcastResult[] | null>(null)
  const [activityById, setActivityById] = useState<Map<string, Activity>>(new Map())
  const [monitorOn, setMonitorOn] = useState(true)
  const [audibleSetName, setAudibleSetName] = useState<string | null>(null)
  const [nowPlayingLabel, setNowPlayingLabel] = useState<string | null>(null)
  const [monitorPending, setMonitorPending] = useState(false)
  const [isPlaying, setIsPlaying] = useState(false)
  const [playbackSeconds, setPlaybackSeconds] = useState(0)

  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const streamRef = useRef<MediaStream | null>(null)
  const startedAtRef = useRef<number>(0)
  const maxTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const targetIdsRef = useRef<string[]>([])
  const monitorAudioRef = useRef<HTMLAudioElement | null>(null)
  const seenCallIdsRef = useRef<Set<number>>(new Set())
  const pendingCallRef = useRef<Call | null>(null)
  const monitorOnRef = useRef(monitorOn)
  const rsVolumeRef = useRef(rsVolume)
  const chirpVolumeRef = useRef(chirpVolume)
  const stateRef = useRef(state)
  const selectedIdsRef = useRef(selectedIds)
  const radioSetsRef = useRef(radioSets)
  const playMonitorCallRef = useRef<(call: Call) => void>(() => {})

  monitorOnRef.current = monitorOn
  rsVolumeRef.current = rsVolume
  chirpVolumeRef.current = chirpVolume
  stateRef.current = state
  selectedIdsRef.current = selectedIds
  radioSetsRef.current = radioSets

  const toggleSet = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const selectAll = () => setSelectedIds(new Set(eligibleSets.map((rs) => rs.id)))
  const selectNone = () => setSelectedIds(new Set())

  const isTransmitting = useCallback(() => {
    const s = stateRef.current
    return s === 'recording' || s === 'uploading'
  }, [])

  const resolveAudibleSetName = useCallback((call: Call): string | null => {
    for (const rs of radioSetsRef.current) {
      if (!selectedIdsRef.current.has(rs.id)) continue
      if (rs.talkgroups.includes(call.talkgroup) || rs.pttTalkgroup === call.talkgroup) {
        return rs.name
      }
    }
    return null
  }, [])

  const clearPlaybackStatus = useCallback(() => {
    setAudibleSetName(null)
    setNowPlayingLabel(null)
    setIsPlaying(false)
    setPlaybackSeconds(0)
  }, [])

  const playMonitorCall = useCallback((call: Call) => {
    if (!monitorOnRef.current) return

    if (isTransmitting()) {
      pendingCallRef.current = call
      setMonitorPending(true)
      return
    }

    const monitor = monitorAudioRef.current
    if (!monitor) return

    if (!monitor.paused) {
      pendingCallRef.current = call
      setMonitorPending(true)
      return
    }

    setMonitorPending(false)
    pendingCallRef.current = null

    const setName = resolveAudibleSetName(call)
    setAudibleSetName(setName)
    setNowPlayingLabel(call.talkgroupLabel || `TG ${call.talkgroup}`)

    monitor.volume = volumeToGain(rsVolumeRef.current)
    monitor.src = `/api/v1/calls/${call.id}/audio?play=1`
    const chirpReady = call.origin === 'ptt'
      ? playChirp(volumeToGain(chirpVolumeRef.current))
      : Promise.resolve()
    chirpReady
      .then(() => monitor.play())
      .catch(() => clearPlaybackStatus())
  }, [clearPlaybackStatus, isTransmitting, resolveAudibleSetName])

  playMonitorCallRef.current = playMonitorCall

  const stopStream = useCallback(() => {
    if (streamRef.current) {
      for (const track of streamRef.current.getTracks()) track.stop()
      streamRef.current = null
    }
    if (maxTimerRef.current) {
      clearTimeout(maxTimerRef.current)
      maxTimerRef.current = null
    }
  }, [])

  const toggleMonitor = useCallback(() => {
    setMonitorOn((prev) => {
      const next = !prev
      if (!next) {
        const monitor = monitorAudioRef.current
        if (monitor) {
          monitor.pause()
          monitor.src = ''
        }
        pendingCallRef.current = null
        setMonitorPending(false)
        clearPlaybackStatus()
      }
      return next
    })
  }, [clearPlaybackStatus])

  const handleMonitorEnded = useCallback(() => {
    clearPlaybackStatus()
    const pending = pendingCallRef.current
    if (!pending || !monitorOnRef.current || isTransmitting()) {
      if (pending) setMonitorPending(true)
      return
    }
    pendingCallRef.current = null
    setMonitorPending(false)
    playMonitorCallRef.current(pending)
  }, [clearPlaybackStatus, isTransmitting])

  useEffect(() => {
    const monitor = monitorAudioRef.current
    if (monitor) {
      monitor.volume = volumeToGain(rsVolume)
    }
  }, [rsVolume])

  const startRecording = useCallback(async () => {
    if (state !== 'idle') return
    if (selectedIds.size === 0) {
      setError('Select at least one radio set first.')
      setState('error')
      globalThis.setTimeout(() => setState((s) => (s === 'error' ? 'idle' : s)), 2000)
      return
    }
    setError('')
    setLastResults(null)
    targetIdsRef.current = Array.from(selectedIds)
    if (monitorAudioRef.current && !monitorAudioRef.current.paused) {
      monitorAudioRef.current.pause()
      clearPlaybackStatus()
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      streamRef.current = stream
      const mimeType = pickPttMimeType()
      const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream)
      chunksRef.current = []
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data)
      }
      recorder.onstop = async () => {
        const elapsed = Date.now() - startedAtRef.current
        stopStream()
        if (elapsed < MIN_DURATION_MS) {
          setState('idle')
          return
        }
        const blob = new Blob(chunksRef.current, { type: pttBlobMimeType(recorder.mimeType) })
        chunksRef.current = []
        setState('uploading')
        try {
          const response = await api.uploadPTTBroadcast(
            targetIdsRef.current,
            blob,
            elapsed / 1000,
            newClientId(),
          )
          setLastResults(response.results)
          setState('idle')
        } catch (err) {
          const message = err instanceof ApiError ? `Broadcast failed (${err.status})` : 'Broadcast failed'
          setError(message)
          setState('error')
          globalThis.setTimeout(() => setState((s) => (s === 'error' ? 'idle' : s)), 2500)
        }
      }
      recorderRef.current = recorder
      startedAtRef.current = Date.now()
      recorder.start()
      setState('recording')
      maxTimerRef.current = globalThis.setTimeout(() => {
        if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
      }, MAX_DURATION_MS)
    } catch (err) {
      stopStream()
      setError(err instanceof Error && err.name === 'NotAllowedError' ? 'Mic permission denied' : 'Mic unavailable')
      setState('error')
      globalThis.setTimeout(() => setState((s) => (s === 'error' ? 'idle' : s)), 2500)
    }
  }, [selectedIds, state, stopStream, clearPlaybackStatus])

  const stopRecording = useCallback(() => {
    if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
  }, [])

  useEffect(() => {
    function shouldIgnoreKey(event: KeyboardEvent): boolean {
      if (event.code !== 'Space') return true
      if (event.repeat) return true
      const target = event.target as HTMLElement | null
      if (!target) return false
      const tag = target.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
      if (target.isContentEditable) return true
      return false
    }
    function onKeyDown(event: KeyboardEvent) {
      if (shouldIgnoreKey(event)) return
      event.preventDefault()
      void startRecording()
    }
    function onKeyUp(event: KeyboardEvent) {
      if (event.code !== 'Space') return
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      event.preventDefault()
      stopRecording()
    }
    globalThis.addEventListener('keydown', onKeyDown)
    globalThis.addEventListener('keyup', onKeyUp)
    return () => {
      globalThis.removeEventListener('keydown', onKeyDown)
      globalThis.removeEventListener('keyup', onKeyUp)
    }
  }, [startRecording, stopRecording])

  useEffect(() => () => stopStream(), [stopStream])

  useEffect(() => {
    if (state !== 'idle' || !pendingCallRef.current || !monitorOnRef.current) return
    const pending = pendingCallRef.current
    pendingCallRef.current = null
    setMonitorPending(false)
    playMonitorCallRef.current(pending)
  }, [state])

  useEffect(() => {
    if (!latestCall) return
    if (seenCallIdsRef.current.has(latestCall.id)) return
    seenCallIdsRef.current.add(latestCall.id)

    const matchingSetIds: string[] = []
    for (const rs of radioSets) {
      if (!selectedIds.has(rs.id)) continue
      const matches =
        rs.talkgroups.includes(latestCall.talkgroup) ||
        rs.pttTalkgroup === latestCall.talkgroup
      if (matches) matchingSetIds.push(rs.id)
    }
    if (matchingSetIds.length === 0) return

    const expiresAt = Date.now() + ACTIVITY_LINGER_MS
    setActivityById((prev) => {
      const next = new Map(prev)
      for (const id of matchingSetIds) {
        next.set(id, { call: latestCall, expiresAt })
      }
      return next
    })

    playMonitorCallRef.current(latestCall)
  }, [latestCall, radioSets, selectedIds])

  useEffect(() => {
    if (activityById.size === 0) return
    const timer = globalThis.setInterval(() => {
      const now = Date.now()
      setActivityById((prev) => {
        let changed = false
        const next = new Map(prev)
        for (const [id, activity] of prev) {
          if (activity.expiresAt <= now) {
            next.delete(id)
            changed = true
          }
        }
        return changed ? next : prev
      })
    }, 500)
    return () => globalThis.clearInterval(timer)
  }, [activityById.size])

  useEffect(() => {
    return () => {
      const monitor = monitorAudioRef.current
      if (monitor) {
        monitor.pause()
        monitor.src = ''
      }
    }
  }, [])

  const buttonLabel: Record<State, string> = {
    idle: `BROADCAST · HOLD TO TALK (${selectedIds.size} set${selectedIds.size === 1 ? '' : 's'})`,
    recording: 'TRANSMITTING TO ALL SELECTED SETS…',
    uploading: 'SENDING…',
    error: error || 'ERROR',
  }
  let buttonColor: string
  if (state === 'recording') {
    buttonColor = 'border-console-error text-console-error bg-console-error/10'
  } else if (state === 'uploading') {
    buttonColor = 'border-console-accent text-console-accent'
  } else if (state === 'error') {
    buttonColor = 'border-console-error text-console-error'
  } else if (selectedIds.size === 0) {
    buttonColor = 'border-console-border text-console-muted opacity-50'
  } else {
    buttonColor = 'border-console-amber text-console-amber hover:bg-console-amber/10'
  }

  return (
    <main className="p-3 sm:p-4 flex flex-col gap-4 max-w-3xl mx-auto">
      <div className="flex items-center justify-between gap-2">
        <button
          onClick={onBack}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-wider hover:border-console-accent hover:text-console-accent"
        >
          ← Radio sets
        </button>
        <span className="text-[10px] text-console-muted uppercase tracking-wider">Dispatcher mode</span>
        <span className="w-[72px]" aria-hidden />
      </div>

      <MonitorAudioBar
        monitorOn={monitorOn}
        onToggleMonitor={toggleMonitor}
        monitorVolume={rsVolume}
        onMonitorVolumeChange={setRsVolume}
        chirpVolume={chirpVolume}
        onChirpVolumeChange={setChirpVolume}
        queueLength={monitorPending ? 1 : 0}
        audibleSetName={audibleSetName}
        nowPlayingLabel={nowPlayingLabel}
        isPlaying={isPlaying}
        playbackSeconds={playbackSeconds}
      />

      <audio
        ref={monitorAudioRef}
        preload="none"
        onPlay={() => setIsPlaying(true)}
        onPause={() => setIsPlaying(false)}
        onEnded={handleMonitorEnded}
        onTimeUpdate={(event) => setPlaybackSeconds(event.currentTarget.currentTime)}
      />

      <div className="border border-console-border rounded p-3 flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[10px] text-console-muted uppercase tracking-wider">
            Target sets — {selectedIds.size} of {eligibleSets.length} selected
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={selectAll}
              className="px-2 py-0.5 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-wider hover:border-console-accent hover:text-console-accent"
            >
              All
            </button>
            <button
              onClick={selectNone}
              className="px-2 py-0.5 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-wider hover:border-console-accent hover:text-console-accent"
            >
              None
            </button>
          </div>
        </div>

        {eligibleSets.length === 0 ? (
          <div className="text-console-error text-xs py-2">
            No radio sets with a PTT talkgroup exist. Create at least one to use dispatcher mode.
          </div>
        ) : (
          <div className="grid gap-1.5 sm:grid-cols-2">
            {eligibleSets.map((rs) => {
              const selected = selectedIds.has(rs.id)
              const activity = selected ? activityById.get(rs.id) : undefined
              const active = activity !== undefined
              let cardClass: string
              if (active) {
                cardClass = 'border-console-accent bg-console-accent/10 text-console-text shadow-[0_0_0_1px_rgba(255,170,0,0.6)_inset] animate-pulse'
              } else if (selected) {
                cardClass = 'border-console-amber bg-console-amber/5 text-console-text'
              } else {
                cardClass = 'border-console-border text-console-muted hover:border-console-accent'
              }
              return (
                <label
                  key={rs.id}
                  className={`flex flex-col gap-1 px-2 py-1.5 border rounded text-xs cursor-pointer select-none ${cardClass}`}
                >
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={selected}
                      onChange={() => toggleSet(rs.id)}
                      className="accent-console-amber"
                    />
                    <span className="flex-1 truncate">{rs.name}</span>
                    <span className="text-[10px] tabular-nums text-console-muted">
                      TG {rs.pttTalkgroup}
                    </span>
                  </div>
                  {active && activity && (
                    <div className="flex items-center gap-2 text-[10px] pl-6">
                      <span className="text-console-accent uppercase tracking-wider">● LIVE</span>
                      <span className="truncate flex-1">
                        {activity.call.talkgroupLabel || `TG ${activity.call.talkgroup}`}
                        {activity.call.origin === 'ptt' && activity.call.senderEmail
                          ? ` · ${activity.call.senderEmail.split('@')[0]}`
                          : ''}
                      </span>
                    </div>
                  )}
                </label>
              )
            })}
          </div>
        )}
      </div>

      <div className="border border-console-border rounded p-6 flex flex-col items-center gap-3">
        <button
          type="button"
          onMouseDown={() => void startRecording()}
          onMouseUp={stopRecording}
          onMouseLeave={() => {
            if (recorderRef.current?.state === 'recording') stopRecording()
          }}
          onTouchStart={(event) => {
            event.preventDefault()
            void startRecording()
          }}
          onTouchEnd={(event) => {
            event.preventDefault()
            stopRecording()
          }}
          onContextMenu={(event) => event.preventDefault()}
          disabled={state === 'uploading' || selectedIds.size === 0}
          className={`w-full px-4 py-4 border-2 rounded text-sm uppercase tracking-wider font-medium select-none disabled:opacity-30 disabled:cursor-not-allowed ${buttonColor}`}
          title="Hold to broadcast (or hold spacebar)"
        >
          {buttonLabel[state]}
        </button>
        <p className="text-[10px] text-console-muted text-center">
          Hold the button or hold the <span className="text-console-accent">SPACEBAR</span> to broadcast to every selected set at once.
        </p>
      </div>

      {lastResults && (
        <div className="border border-console-border rounded p-3">
          <div className="text-[10px] text-console-muted uppercase tracking-wider mb-2">
            Last broadcast — {lastResults.filter((r) => !r.error).length} of {lastResults.length} delivered
          </div>
          <div className="flex flex-col gap-1">
            {lastResults.map((result) => {
              const set = radioSets.find((rs) => rs.id === result.radioSetId)
              const setName = set?.name ?? result.radioSetId
              const ok = !result.error
              return (
                <div
                  key={result.radioSetId}
                  className={`flex items-center justify-between gap-2 text-xs px-2 py-1 rounded ${
                    ok ? 'text-console-accent' : 'text-console-error'
                  }`}
                >
                  <span className="truncate">{ok ? '✓' : '✗'} {setName}</span>
                  <span className="text-[10px] tabular-nums">
                    {ok ? `call ${result.callId}` : result.error}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </main>
  )
}
