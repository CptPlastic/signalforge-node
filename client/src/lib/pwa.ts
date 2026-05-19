export function registerPWA() {
  if (!('serviceWorker' in navigator)) return
  if (globalThis.location.hostname === 'localhost' && globalThis.location.port === '5173') return

  globalThis.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch((error: unknown) => {
      console.warn('PWA registration failed', error)
    })
  })
}
