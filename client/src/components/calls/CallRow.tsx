import type { Call } from '../../lib/api'
import { fmtDur, fmtFreq, fmtTime } from '../../lib/format'

export type CallRowProps = Readonly<{
  call: Call
  active: boolean
  playing: boolean
  favorite: boolean
  muted: boolean
  saved: boolean
  onPlay: () => void
  onToggleFavorite: () => void
  onToggleMuted: () => void
  onToggleSaved: () => void
}>

function getStatusLabel(active: boolean, playing: boolean): string {
  if (playing) return 'PLAYING'
  if (active) return 'ACTIVE'
  return ''
}

function getRowClass(active: boolean, playing: boolean): string {
  if (playing) {
    return 'border-console-accent/70 bg-console-accent/15 shadow-[inset_3px_0_0_rgba(0,255,65,0.85)]'
  }
  if (active) {
    return 'border-console-accent/50 bg-console-accent/10 shadow-[inset_3px_0_0_rgba(0,255,65,0.45)]'
  }
  return 'border-console-border hover:bg-console-surface/60'
}

function transcriptLabel(call: Call): string {
  if (call.transcriptText) return call.transcriptText
  if (call.transcriptStatus === 'processing') return '[processing]'
  if (call.transcriptStatus === 'pending') return '[queued]'
  if (call.transcriptStatus === 'skipped') return '[skipped]'
  if (call.transcriptStatus === 'failed') return '[failed]'
  return '—'
}

function transcriptBadge(call: Call): { label: string; className: string } | null {
  const status = call.transcriptText ? 'done' : call.transcriptStatus
  if (status === 'done') return { label: 'TX', className: 'border-console-accent text-console-accent' }
  if (status === 'processing') return { label: 'RUN', className: 'border-console-amber text-console-amber' }
  if (status === 'pending') return { label: 'QUE', className: 'border-console-amber text-console-amber' }
  if (status === 'skipped') return { label: 'SKIP', className: 'border-console-muted text-console-muted' }
  if (status === 'failed') return { label: 'ERR', className: 'border-console-error text-console-error' }
  return null
}

export function CallRow({
  call,
  active,
  playing,
  favorite,
  muted,
  saved,
  onPlay,
  onToggleFavorite,
  onToggleMuted,
  onToggleSaved,
}: CallRowProps) {
  const isRedacted = call.redacted === true
  const playLabel = playing ? '▶ PLAY' : '▶'
  const playAriaLabel = playing ? 'playing' : 'play call'
  const statusLabel = getStatusLabel(active, playing)
  const rowClass = getRowClass(active, playing)
  const transcript = transcriptLabel(call)
  const transcriptState = transcriptBadge(call)

  return (
    <tr
      className={`border-b text-xs transition-colors ${rowClass} ${muted ? 'opacity-50' : ''}`}
    >
      <td className="py-2 px-2">
        <input
          type="checkbox"
          checked={saved}
          onChange={onToggleSaved}
          aria-label="save call to call box"
        />
      </td>
      <td className="py-2 px-3 text-console-muted tabular-nums whitespace-nowrap">
        <div className="flex items-center gap-2">
          <span>{fmtTime(call.dateTime)}</span>
          {statusLabel && (
            <span className="px-1.5 py-0.5 border border-console-accent/60 rounded text-[9px] text-console-accent uppercase tracking-widest">
              {statusLabel}
            </span>
          )}
        </div>
      </td>
      <td className="py-2 px-3 font-semibold text-console-accent tabular-nums">
        <div className="flex items-center gap-2">
          {call.talkgroup > 0 ? call.talkgroup : '—'}
          <button
            onClick={onToggleFavorite}
            disabled={isRedacted}
            className={`text-[10px] px-1.5 py-0.5 border rounded ${favorite ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
            aria-label={favorite ? 'unfavorite talkgroup' : 'favorite talkgroup'}
            title={favorite ? 'Favorited talkgroup' : 'Mark as favorite'}
          >
            ★
          </button>
          <button
            onClick={onToggleMuted}
            disabled={isRedacted}
            className={`text-[10px] px-1.5 py-0.5 border rounded ${muted ? 'border-console-error text-console-error' : 'border-console-border text-console-muted'}`}
            aria-label={muted ? 'unmute talkgroup' : 'mute talkgroup'}
            title={muted ? 'Muted talkgroup' : 'Mute talkgroup'}
          >
            M
          </button>
        </div>
      </td>
      <td className="py-2 px-3 max-w-[180px] truncate" title={call.talkgroupLabel}>
        {call.talkgroupLabel || <span className="text-console-muted">—</span>}
      </td>
      <td className="py-2 px-3 text-console-muted" title={call.talkgroupGroup}>
        {call.talkgroupGroup || '—'}
      </td>
      <td className="py-2 px-3 max-w-[140px] truncate" title={call.systemLabel}>
        {call.systemLabel || <span className="text-console-muted">—</span>}
      </td>
      <td className="py-2 px-3 tabular-nums text-console-muted whitespace-nowrap">
        {fmtFreq(call.frequency)}
      </td>
      <td className="py-2 px-3 tabular-nums text-console-muted">{fmtDur(call.duration)}</td>
      <td className="py-2 px-3 text-console-muted">
        <div className="flex items-start gap-2">
          {transcriptState && (
            <span className={`flex-shrink-0 px-1.5 py-0.5 border rounded text-[9px] leading-none tabular-nums mt-0.5 ${transcriptState.className}`}>
              {transcriptState.label}
            </span>
          )}
          <span className={`break-words whitespace-normal leading-relaxed ${call.transcriptText ? 'text-console-text' : ''}`}>{transcript}</span>
        </div>
      </td>
      <td className="py-2 px-3">
        <div className="flex items-center gap-1.5">
          <button
            onClick={onPlay}
            disabled={isRedacted}
            className={`px-2 py-0.5 border rounded text-[10px] uppercase tracking-widest transition-colors ${
              playing
                ? 'border-console-accent text-console-accent'
                : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
            }`}
            aria-label={isRedacted ? 'call redacted' : playAriaLabel}
          >
            {isRedacted ? 'LOCKED' : playLabel}
          </button>
          {isRedacted ? (
            <span className="px-2 py-0.5 border border-console-border rounded text-[10px] text-console-muted">—</span>
          ) : (
            <a
              href={`/api/v1/calls/${call.id}/audio?download=1`}
              download={call.audioName || `call-${call.id}`}
              className="px-2 py-0.5 border border-console-border rounded text-[10px] text-console-muted hover:border-console-accent hover:text-console-accent"
              aria-label="download call audio"
              title="Download"
            >
              ↓
            </a>
          )}
        </div>
      </td>
    </tr>
  )
}
