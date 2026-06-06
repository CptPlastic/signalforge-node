/** Hub console display modes — see https://signalforge.org/DISPLAY-MODES.md */

export type DisplayMode = 'dark' | 'nite' | 'nvg' | 'light'

export const DISPLAY_MODE_STORAGE_KEY = 'sf-display-mode'

export const TACTICAL_MODES: readonly DisplayMode[] = ['dark', 'nite', 'nvg'] as const

export const DISPLAY_MODE_LABELS: Record<DisplayMode, string> = {
  dark: 'DARK',
  nite: 'NITE',
  nvg: 'NVG',
  light: 'LIGHT',
}

export const DISPLAY_MODE_ICONS: Record<DisplayMode, string> = {
  dark: '◑',
  nite: '▌',
  nvg: '◈',
  light: '☀',
}

export function isDisplayMode(value: string | null | undefined): value is DisplayMode {
  return value === 'dark' || value === 'nite' || value === 'nvg' || value === 'light'
}

export function getStoredDisplayMode(): DisplayMode {
  try {
    const stored = localStorage.getItem(DISPLAY_MODE_STORAGE_KEY)
    return isDisplayMode(stored) ? stored : 'dark'
  } catch {
    return 'dark'
  }
}

export function applyDisplayMode(mode: DisplayMode): void {
  document.documentElement.dataset.sfDisplayMode = mode
  try {
    localStorage.setItem(DISPLAY_MODE_STORAGE_KEY, mode)
  } catch {
    // ignore
  }
}

export function initDisplayMode(): DisplayMode {
  const mode = getStoredDisplayMode()
  applyDisplayMode(mode)
  return mode
}

export function nextTacticalMode(current: DisplayMode): DisplayMode {
  const index = TACTICAL_MODES.indexOf(current)
  if (index < 0) return 'dark'
  return TACTICAL_MODES[(index + 1) % TACTICAL_MODES.length]!
}
