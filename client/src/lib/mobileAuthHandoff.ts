/** True for phone/tablet browsers that should prefer the native SignalForge app. */
export function isMobileUserAgent(): boolean {
  return /iPhone|iPad|iPod|Android/i.test(globalThis.navigator.userAgent)
}

export function signalforgeAppSignInUrl(hubOrigin: string, token: string): string {
  return `signalforge://signin?hub=${encodeURIComponent(hubOrigin)}&token=${encodeURIComponent(token)}`
}

export function tryOpenSignalforgeApp(hubOrigin: string, token: string): void {
  globalThis.window.location.href = signalforgeAppSignInUrl(hubOrigin, token)
}
