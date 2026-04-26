// v5.0.4 — app-shell switched to network-first with cache fallback so
// installed PWAs always pick up new app.js / index.html / style.css the
// next time they're online. Cache-first was holding installed clients on
// pre-BL187 nav HTML even after the daemon shipped the fix.
// Cache name bumped to invalidate every existing install on next activate.
const CACHE_NAME = 'datawatch-v5-4-0';
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
  // App shell + docs + diagrams: network-first with cache fallback so
  // upgrades land immediately and offline still works.
  const isAppShell =
    url.pathname === '/' ||
    url.pathname === '/index.html' ||
    url.pathname === '/app.js' ||
    url.pathname === '/style.css' ||
    url.pathname === '/manifest.json' ||
    url.pathname === '/diagrams.html' ||
    url.pathname.startsWith('/docs/');
  if (isAppShell) {
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
  // Everything else (icons, fonts, xterm.css, etc.): cache-first, network fallback.
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
