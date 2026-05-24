const CACHE_NAME = 'p7-scanner-shell-v3'
const APP_SHELL = ['/', '/index.html', '/offline.html', '/manifest.webmanifest', '/pwa/icon.svg']

function cacheShellResponse(cacheKey, response) {
  if (response?.status === 200) {
    const copy = response.clone()
    caches.open(CACHE_NAME).then((cache) => cache.put(cacheKey, copy))
  }
  return response
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((cache) => cache.addAll(APP_SHELL))
      .then(() => globalThis.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.map((key) => (key === CACHE_NAME ? undefined : caches.delete(key)))))
      .then(() => globalThis.clients.claim()),
  )
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  const url = new URL(request.url)

  if (request.method !== 'GET') {
    return
  }

  if (url.origin !== globalThis.location.origin) {
    return
  }

  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws') || url.pathname.startsWith('/public/')) {
    return
  }

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((response) => cacheShellResponse('/index.html', response))
        .catch(() => caches.match('/index.html').then((response) => response || caches.match('/offline.html'))),
    )
    return
  }

  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) {
        return cached
      }
      return fetch(request).then((response) => cacheShellResponse(request, response))
    }),
  )
})
