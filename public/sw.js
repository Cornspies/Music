/* Music service worker — dev-safe offline app shell.
 *
 * Goals:
 *  - offline app shell (cached "/" navigation fallback + static assets)
 *  - NEVER interfere with Next.js dev HMR, hashed assets, or any
 *    API / audio / auth traffic going through the Caddy gateway.
 *
 * Strategy summary:
 *  - non-GET requests            -> ignored (browser handles)
 *  - /api/*                      -> passed through (never cached)
 *  - anything with /_next/       -> passed through (HMR + hashed assets)
 *  - ?XTransformPort=...         -> passed through (gateway media/API)
 *  - cross-origin                -> passed through
 *  - navigations                 -> network-first, cache copy at "/", offline fallback
 *  - static shell assets         -> cache-first
 *  - everything else             -> passed through
 */
const VERSION = "music-v1"
const CACHE = VERSION

const PRECACHE = [
  "/",
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/icon-maskable-512.png",
]

const STATIC_ASSETS = [
  "/manifest.webmanifest",
  "/logo.svg",
  "/robots.txt",
  "/favicon.ico",
]

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) =>
      // Precache each entry individually with per-URL tolerance, so one
      // flaky dev-server response never blocks installation.
      Promise.all(
        PRECACHE.map((url) =>
          cache.add(url).catch(() => {
            /* best-effort precache */
          })
        )
      )
    )
  )
})

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
      )
      .then(() => self.clients.claim())
  )
})

self.addEventListener("fetch", (event) => {
  const req = event.request

  // Only ever touch GET requests; POST/PATCH/DELETE/etc. must reach the server.
  if (req.method !== "GET") return

  let url
  try {
    url = new URL(req.url)
  } catch {
    return
  }

  // Cross-origin requests: not ours, let the browser handle them.
  if (url.origin !== self.location.origin) return

  // Websocket upgrades (dev HMR or anything else): hands off.
  if (req.mode === "websocket") return

  // API traffic (auth, tracks, streaming, covers): NEVER cache or intercept.
  if (url.pathname.startsWith("/api/")) return

  // Next.js internals: HMR, /_next/image, hashed static assets. Also covers
  // anything like /foo/_next/... — pass through to keep dev HMR alive.
  if (url.pathname.includes("/_next/")) return

  // Gateway-proxied requests (audio/cover/API via Caddy): pass through.
  if (url.search.includes("XTransformPort")) return

  // Navigations: network-first so dev HMR + fresh HTML always win online;
  // cache the copy under "/" as the offline app-shell fallback.
  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req)
        .then((response) => {
          if (response && response.ok) {
            const copy = response.clone()
            event.waitUntil(
              caches.open(CACHE).then((cache) => cache.put("/", copy))
            )
          }
          return response
        })
        .catch(() =>
          caches.match("/").then(
            (hit) =>
              hit ||
              new Response(
                "<!doctype html><title>Music — offline</title><body style=\"background:#09090b;color:#34d399;font-family:system-ui;display:grid;place-items:center;height:100vh;margin:0\"><p>Music is offline. Reconnect to keep the music playing.</p></body>",
                { headers: { "Content-Type": "text/html; charset=utf-8" } }
              )
          )
        )
    )
    return
  }

  // Same-origin static shell assets: cache-first.
  if (url.pathname.startsWith("/icons/") || STATIC_ASSETS.includes(url.pathname)) {
    event.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((response) => {
            if (response && response.ok) {
              const copy = response.clone()
              event.waitUntil(
                caches.open(CACHE).then((cache) => cache.put(req, copy))
              )
            }
            return response
          })
      )
    )
    return
  }

  // Everything else (unknown paths, dev-only responses, etc.): pass through
  // by not calling respondWith — the browser talks to the network directly.
})
