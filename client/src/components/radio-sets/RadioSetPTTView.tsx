import { useEffect, useState } from 'react'
import type { RadioSet } from '../../lib/api'
import { PTTButton } from './PTTButton'

type Props = Readonly<{
  radioSet: RadioSet
  onBack: () => void
}>

function fmtTime(ts: number): string {
  const d = new Date(ts)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function RadioSetPTTView({ radioSet, onBack }: Props) {
  const [inputDevices, setInputDevices] = useState<MediaDeviceInfo[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState<string>('')
  const [lastSent, setLastSent] = useState<{ durationSeconds: number; at: number } | null>(null)

  useEffect(() => {
    let cancelled = false
    async function refreshDevices() {
      try {
        const all = await navigator.mediaDevices.enumerateDevices()
        if (cancelled) return
        setInputDevices(all.filter((d) => d.kind === 'audioinput'))
      } catch {
        // permission probably not granted yet — labels will populate after first recording
      }
    }
    void refreshDevices()
    navigator.mediaDevices.addEventListener?.('devicechange', refreshDevices)
    return () => {
      cancelled = true
      navigator.mediaDevices.removeEventListener?.('devicechange', refreshDevices)
    }
  }, [])

  const hasPTTTalkgroup = radioSet.pttTalkgroup !== undefined

  return (
    <main className="p-3 sm:p-4 flex flex-col gap-4 max-w-2xl mx-auto">
      <div className="flex items-center justify-between gap-2">
        <button
          onClick={onBack}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] uppercase tracking-wider hover:border-console-accent hover:text-console-accent"
        >
          ← Radio sets
        </button>
        <span className="text-[10px] text-console-muted uppercase tracking-wider">PTT mode</span>
      </div>

      <div className="border border-console-border rounded p-4 flex flex-col gap-1">
        <h2 className="text-console-accent text-lg font-medium">{radioSet.name}</h2>
        <div className="text-xs text-console-muted flex flex-wrap gap-x-4 gap-y-0.5">
          <span>
            PTT talkgroup:{' '}
            {hasPTTTalkgroup ? (
              <span className="text-console-accent">{radioSet.pttTalkgroup}</span>
            ) : (
              <span className="text-console-error">not allocated</span>
            )}
          </span>
          <span>Listening to {radioSet.talkgroups.length} talkgroup{radioSet.talkgroups.length === 1 ? '' : 's'}</span>
        </div>
      </div>

      <div className="border border-console-border rounded p-3 flex flex-col gap-2">
        <label className="text-[10px] text-console-muted uppercase tracking-wider">Microphone</label>
        <select
          value={selectedDeviceId}
          onChange={(event) => setSelectedDeviceId(event.target.value)}
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
        >
          <option value="">System default</option>
          {inputDevices.map((device, index) => (
            <option key={device.deviceId || `dev-${index}`} value={device.deviceId}>
              {device.label || `Input ${index + 1}`}
            </option>
          ))}
        </select>
        {inputDevices.every((d) => !d.label) && (
          <p className="text-[10px] text-console-muted">
            Device names appear after the first transmission (browser mic permission).
          </p>
        )}
      </div>

      <div className="border border-console-border rounded p-6 flex flex-col items-center gap-4">
        {hasPTTTalkgroup ? (
          <>
            <PTTButton
              radioSetId={radioSet.id}
              enableSpacebar
              deviceId={selectedDeviceId || undefined}
              onTransmitted={(info) => setLastSent(info)}
            />
            <p className="text-[10px] text-console-muted text-center">
              Hold the button or hold the <span className="text-console-accent">SPACEBAR</span> to transmit.
            </p>
          </>
        ) : (
          <p className="text-console-error text-xs text-center">
            This radio set has no PTT talkgroup allocated. Recreate the set, or run the backfill, to enable PTT.
          </p>
        )}
      </div>

      <div className="border border-console-border rounded p-3">
        <div className="text-[10px] text-console-muted uppercase tracking-wider mb-1">Last sent</div>
        {lastSent ? (
          <div className="text-xs text-console-accent tabular-nums">
            {fmtTime(lastSent.at)} · {lastSent.durationSeconds.toFixed(1)}s
          </div>
        ) : (
          <div className="text-xs text-console-muted">No transmissions yet this session.</div>
        )}
      </div>
    </main>
  )
}
