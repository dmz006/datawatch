// v4.0.7 — bumped cache name to invalidate the v4.0.3–v4.0.6 app.js
// and diagrams.html copies that were being served stale out of the
// service-worker cache. /docs/* and /diagrams.html now go
// network-first so fresh docs show up without forcing a hard-reload;
// everything else stays cache-first for offline PWA support.
const CACHE_NAME = 'datawatch-v2';
const STATIC_ASSETS = ['/', '/index.html', '/app.js', '/style.css', '/manifest.json'];

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => cache.addAll(STATIC_ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys =>
      Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  // API + WebSocket: always network, never cache.
  if (url.pathname.startsWith('/api/') || url.pathname === '/ws') {
    return;
  }
  // Docs tree + diagrams viewer: network-first with a cached
  // fallback so new docs appear without a hard-reload.
  if (url.pathname.startsWith('/docs/') || url.pathname === '/diagrams.html') {
    event.respondWith(
      fetch(event.request)
        .then(resp => {
          const copy = resp.clone();
          caches.open(CACHE_NAME).then(c => c.put(event.request, copy)).catch(() => {});
          return resp;
        })
        .catch(() => caches.match(event.request).then(r => r || new Response('offline', { status: 503 })))
    );
    return;
  }
  // Everything else: cache-first, network fallback.
  event.respondWith(
    caches.match(event.request).then(cached => cached || fetch(event.request))
  );
});

self.addEventListener('push', event => {
  const data = event.data ? event.data.json() : {};
  event.waitUntil(
    self.registration.showNotification(data.title || 'Datawatch', {
      body: data.body || 'A session needs your attention.',
      icon: '/icon-192.svg',
      badge: '/icon-192.svg',
      tag: data.tag || 'datawatch',
      renotify: true,
    })
  );
});

self.addEventListener('notificationclick', event => {
  event.notification.close();
  event.waitUntil(clients.openWindow('/'));
});
