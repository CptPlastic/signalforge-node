import type { Call } from '../../lib/api'
import { fmtDur, fmtFreq, fmtTime } from '../../lib/format'

export type CallRowProps = Readonly<{
  call: Call
  playing: boolean
  favorite: boolean
  muted: boolean
  saved: boolean
  onPlay: () => void
  onToggleFavorite: () => void
  onToggleMuted: () => void
  onToggleSaved: () => void
}>

export function CallRow({
  call,
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
  return (
    <tr
      className={`border-b border-console-border text-xs transition-colors ${playing ? 'bg-console-surface' : 'hover:bg-console-surface/60'} ${muted ? 'opacity-50' : ''}`}
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
        {fmtTime(call.dateTime)}
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
