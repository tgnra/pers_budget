// Service worker: caches versioned static assets; pages always come from the network
// (budget data lives on the server), with a plain offline notice as fallback.
const CACHE = 'budget-static';

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', e => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== location.origin) return;

  if (url.pathname.startsWith('/static/') || url.pathname.endsWith('.png')) {
    e.respondWith(caches.open(CACHE).then(async c => {
      const hit = await c.match(req);
      if (hit) return hit;
      const res = await fetch(req);
      if (res.ok) c.put(req, res.clone());
      return res;
    }));
    return;
  }

  if (req.mode === 'navigate') {
    e.respondWith(fetch(req).catch(() => new Response(
      '<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">' +
      '<body style="font:16px system-ui;padding:24px">Offline — reconnect to load your budget.</body>',
      { headers: { 'Content-Type': 'text/html; charset=utf-8' } })));
  }
});
