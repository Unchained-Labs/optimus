// optimus service worker: makes the dashboard installable and opens the app
// shell instantly. Live data (/api) always goes to the network.
const SHELL = "optimus-shell-v1";
const ASSETS = ["/", "/static/app.css", "/static/app.js", "/static/vendor/xterm.js", "/static/vendor/xterm.css", "/static/vendor/addon-fit.js", "/static/icon-192.png"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(SHELL).then((c) => c.addAll(ASSETS)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys().then((ks) => Promise.all(ks.filter((k) => k !== SHELL).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.pathname.startsWith("/api/") || url.search.includes("token=")) return;
  // network first, so a new optimus version shows up right away; cache as fallback
  e.respondWith(
    fetch(e.request)
      .then((r) => { const copy = r.clone(); caches.open(SHELL).then((c) => c.put(e.request, copy)); return r; })
      .catch(() => caches.match(e.request))
  );
});

self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  e.waitUntil(self.clients.matchAll({ type: "window" }).then((cs) => (cs[0] ? cs[0].focus() : self.clients.openWindow("/"))));
});
