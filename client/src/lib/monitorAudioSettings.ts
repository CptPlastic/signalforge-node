export const CHIRP_VOLUME_STORAGE_KEY = 'p7_chirp_volume'

export const DEFAULT_MONITOR_VOLUME = 100
export const DEFAULT_CHIRP_VOLUME = 15

export function clampVolume(value: number, fallback: number): number {
  if (!Number.isFinite(value)) return fallback
  return Math.min(100, Math.max(1, Math.round(value)))
}

export function volumeToGain(volume: number): number {
  return clampVolume(volume, DEFAULT_MONITOR_VOLUME) / 100
}

export function getStoredChirpVolume(): number {
  const stored = localStorage.getItem(CHIRP_VOLUME_STORAGE_KEY)
  if (stored == null) return DEFAULT_CHIRP_VOLUME
  const saved = Number(stored)
  if (!Number.isFinite(saved) || saved <= 0) return DEFAULT_CHIRP_VOLUME
  return clampVolume(saved, DEFAULT_CHIRP_VOLUME)
}

export function storeChirpVolume(value: number): void {
  localStorage.setItem(CHIRP_VOLUME_STORAGE_KEY, String(clampVolume(value, DEFAULT_CHIRP_VOLUME)))
}
