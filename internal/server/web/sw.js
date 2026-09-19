// The dashboard runs in its own window, so a restart of the trainer must not show the
// browser's "can't reach this site" page. The shell is kept in a cache and served when the
// server is away; app.js then shows a reconnecting overlay until it answers again.
const CACHE = 'shell-v3';
const SHELL = ['/', '/app.css', '/app.js', '/events-worker.js', '/stats.html', '/rules.html', '/hud.html', '/ai.html', '/settings.html', '/icon.svg', '/fonts/RussoOne-Regular.ttf'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(caches.keys()
    .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
    .then(() => self.clients.claim()));
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  const skip = e.request.method !== 'GET' || url.origin !== location.origin ||
    url.pathname.startsWith('/api/') || url.pathname === '/events';
  if (skip) return;
  e.respondWith(fetch(e.request).then((res) => {
    if (res.ok) {
      const copy = res.clone();
      caches.open(CACHE).then((c) => c.put(e.request, copy));
    }
    return res;
  }).catch(async () => {
    const hit = await caches.match(e.request) || (e.request.mode === 'navigate' ? await caches.match('/') : null);
    return hit || Response.error();
  }));
});
