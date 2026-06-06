import { useCallback, useEffect, useRef, useState } from 'react'
import {
  DISPLAY_MODE_ICONS,
  DISPLAY_MODE_LABELS,
  TACTICAL_MODES,
  applyDisplayMode,
  getStoredDisplayMode,
  initDisplayMode,
  nextTacticalMode,
  type DisplayMode,
} from '../lib/displayModes'

const LONG_PRESS_MS = 800

export function DisplayModeSwitcher({ compact = false }: Readonly<{ compact?: boolean }>) {
  const [mode, setMode] = useState<DisplayMode>(() => initDisplayMode())
  const darkTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    setMode(getStoredDisplayMode())
  }, [])

  const selectMode = useCallback((target: DisplayMode) => {
    if (target === 'light') {
      const next = mode === 'light' ? 'dark' : 'light'
      applyDisplayMode(next)
      setMode(next)
      return
    }
    if (mode === target) {
      const next = nextTacticalMode(target)
      applyDisplayMode(next)
      setMode(next)
      return
    }
    applyDisplayMode(target)
    setMode(target)
  }, [mode])

  const clearDarkTimer = () => {
    if (darkTimer.current) {
      clearTimeout(darkTimer.current)
      darkTimer.current = null
    }
  }

  const modes: DisplayMode[] = [...TACTICAL_MODES, 'light']

  return (
    <div className={compact ? 'flex flex-wrap gap-1' : 'flex flex-col gap-1'}>
      <div className="flex flex-wrap gap-1" role="group" aria-label="Display mode">
        {modes.map((m) => {
          const active = mode === m
          const isDark = m === 'dark'
          return (
            <button
              key={m}
              type="button"
              aria-pressed={active}
              className={[
                'px-2 py-1 border rounded uppercase tracking-wider text-[10px] font-bold transition-colors',
                active
                  ? 'border-console-accent text-console-accent bg-console-accent/10'
                  : 'border-console-border text-console-muted hover:text-console-accent hover:border-console-accent/40',
              ].join(' ')}
              onClick={() => selectMode(m)}
              onMouseDown={() => {
                if (!isDark) return
                clearDarkTimer()
                darkTimer.current = setTimeout(() => {
                  applyDisplayMode('light')
                  setMode('light')
                }, LONG_PRESS_MS)
              }}
              onMouseUp={clearDarkTimer}
              onMouseLeave={clearDarkTimer}
              onTouchStart={() => {
                if (!isDark) return
                clearDarkTimer()
                darkTimer.current = setTimeout(() => {
                  applyDisplayMode('light')
                  setMode('light')
                }, LONG_PRESS_MS)
              }}
              onTouchEnd={clearDarkTimer}
            >
              {DISPLAY_MODE_ICONS[m]} {DISPLAY_MODE_LABELS[m]}
            </button>
          )
        })}
      </div>
      {!compact && (
        <span className="console-label text-[9px] opacity-80">
          TAP LIGHT FOR DAY · HOLD DARK ON TOUCH · TACTICAL CYCLE SKIPS BRIGHTNESS
        </span>
      )}
    </div>
  )
}
