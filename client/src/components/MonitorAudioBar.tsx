type VolumeStepperProps = Readonly<{
  label: string
  value: number
  onChange: (value: number) => void
}>

function VolumeStepper({ label, value, onChange }: VolumeStepperProps) {
  const step = (delta: number) => onChange(Math.min(100, Math.max(1, value + delta)))

  return (
    <div className="flex items-center gap-2">
      <span className="w-11 text-[10px] uppercase tracking-wider text-console-muted">{label}</span>
      <button
        type="button"
        onClick={() => step(-5)}
        className="w-7 h-7 border border-console-border text-console-accent text-base leading-none hover:border-console-accent"
        aria-label={`Decrease ${label}`}
      >
        −
      </button>
      <div className="flex-1 h-2 border border-console-border bg-console-bg">
        <div
          className="h-full bg-console-accent opacity-85"
          style={{ width: `${value}%` }}
        />
      </div>
      <button
        type="button"
        onClick={() => step(5)}
        className="w-7 h-7 border border-console-border text-console-accent text-base leading-none hover:border-console-accent"
        aria-label={`Increase ${label}`}
      >
        +
      </button>
      <span className="w-10 text-right tabular-nums text-[11px] text-console-accent">{value}%</span>
    </div>
  )
}

function formatElapsed(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const mm = Math.floor(s / 60)
  const ss = s % 60
  return `${mm}:${ss.toString().padStart(2, '0')}`
}

type MonitorAudioBarProps = Readonly<{
  monitorOn: boolean
  onToggleMonitor: () => void
  monitorVolume: number
  onMonitorVolumeChange: (value: number) => void
  chirpVolume: number
  onChirpVolumeChange: (value: number) => void
  queueLength?: number
  nowPlayingLabel?: string | null
  audibleSetName?: string | null
  isPlaying?: boolean
  playbackSeconds?: number
}>

export function MonitorAudioBar({
  monitorOn,
  onToggleMonitor,
  monitorVolume,
  onMonitorVolumeChange,
  chirpVolume,
  onChirpVolumeChange,
  queueLength = 0,
  nowPlayingLabel,
  audibleSetName,
  isPlaying = false,
  playbackSeconds = 0,
}: MonitorAudioBarProps) {
  const statusParts: string[] = []
  if (audibleSetName) statusParts.push(audibleSetName)
  if (nowPlayingLabel) statusParts.push(nowPlayingLabel)
  if (isPlaying && playbackSeconds > 0) statusParts.push(formatElapsed(playbackSeconds))
  if (queueLength > 0) statusParts.push(`${queueLength} queued`)

  let statusLine = 'MONITOR OFF'
  if (statusParts.length > 0) {
    statusLine = statusParts.join(' · ')
  } else if (monitorOn) {
    statusLine = 'MONITOR READY'
  }

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-2 bg-console-surface">
      <div className="flex items-center gap-2.5 min-w-0">
        <button
          type="button"
          onClick={onToggleMonitor}
          className={`px-2.5 py-1 border text-[11px] tracking-widest uppercase flex-shrink-0 ${
            monitorOn
              ? 'border-console-accent text-console-accent bg-console-accent/10'
              : 'border-console-border text-console-muted hover:border-console-accent'
          }`}
        >
          {monitorOn ? '◉ MON' : '○ MON'}
        </button>
        <span className="flex-1 truncate text-[11px] tracking-wide text-console-text font-mono">
          {statusLine}
        </span>
      </div>
      <VolumeStepper label="VOL" value={monitorVolume} onChange={onMonitorVolumeChange} />
      <VolumeStepper label="CHIRP" value={chirpVolume} onChange={onChirpVolumeChange} />
    </div>
  )
}
