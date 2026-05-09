// v5.0.4 — app-shell switched to network-first with cache fallback so
// installed PWAs always pick up new app.js / index.html / style.css the
// next time they're online. Cache-first was holding installed clients on
// pre-BL187 nav HTML even after the daemon shipped the fix.
//
// v5.26.6 — cache name bumped from v5-6-1 → v5-26-6. Operator-reported
// regression: Autonomous tab buttons silently no-op'd because installed
// PWAs were still running pre-v5.26.3 cached app.js (the buggy
// renderPRDActions where JSON.stringify outputs broke the onclick
// attribute). Network-first SHOULD have picked up the fix on the next
// online fetch, but installed PWAs that hit a transient offline window
// during the v5.7→v5.26 stretch ended up serving the stale cached
// build. Bumping CACHE_NAME forces every install to drop the v5-6-1
// cache on next activate. Same pattern as BL187/v5.0.4.
// v6.13.13 — bumped each release so installed PWAs see a different SW
// and trigger install → activate → forced navigate. The activate
// handler below now calls Client.navigate(client.url) on every
// controlled window after the cache purge, which forces the page to
// reload from the network. This works WITHOUT the OLD app.js needing
// a controllerchange listener — the SW drives the navigation directly.
const CACHE_NAME = 'datawatch-v7-0-0-alpha-14';
const STATIC_ASSETS = ['/', '/index.html', '/app.js', '/style.css', '/manifest.json'];

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => cache.addAll(STATIC_ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  // v6.13.13 — operator: "I can't expect users to clear cache on
  // upgrades, how can it be better". After the new SW activates, force
  // every controlled window to navigate to its current URL. This
  // bypasses the OLD app.js entirely (no need for it to have a
  // controllerchange listener) and pulls fresh /index.html, /app.js,
  // /style.css from the daemon — which now ship with proper
  // Cache-Control headers (v6.13.12). Result: PWA users see new
  // releases the moment they reopen the app, with no manual refresh.
  event.waitUntil((async () => {
    // Purge old caches.
    const keys = await caches.keys();
    await Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)));
    // Take control of every existing tab.
    await self.clients.claim();
    // Force every window to reload from network. WindowClient.navigate
    // requires the SW to be the controller (just claimed above) and the
    // target URL to be same-origin (always true here).
    const windows = await self.clients.matchAll({ type: 'window' });
    for (const w of windows) {
      try { await w.navigate(w.url); } catch (e) { /* ignore */ }
    }
  })());
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
