const CACHE = 'whisper-v1';
const PRECACHE = ['/', '/chat', '/css/style.css', '/js/login.js', '/js/chat.js'];

self.addEventListener('install', (e) => {
    e.waitUntil(caches.open(CACHE).then((c) => c.addAll(PRECACHE)));
    self.skipWaiting();
});

self.addEventListener('activate', (e) => {
    e.waitUntil(
        caches.keys().then((keys) =>
            Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
        )
    );
    self.clients.claim();
});

self.addEventListener('fetch', (e) => {
    if (e.request.method !== 'GET') return;
    if (e.request.url.includes('/api/') || e.request.url.includes('/ws')) return;
    e.respondWith(
        fetch(e.request).catch(() => caches.match(e.request))
    );
});
