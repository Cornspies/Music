/* Music service worker — offline app shell + offline playback.
 *
 * Goals:
 *  - offline app shell (cached "/" navigation fallback + static assets)
 *  - serve downloaded tracks (see src/lib/offline.ts) for /api/tracks/<id>/stream
 *    and /cover, with Range support so seeking works
 *  - NEVER interfere with Next.js dev HMR, hashed assets, or any other
 *    API / auth traffic
 *
 * Strategy summary:
 *  - non-GET requests            -> ignored (browser handles)
 *  - /api/tracks/<id>/stream     -> offline cache if downloaded, else network
 *  - /api/tracks/<id>/cover      -> offline cache if downloaded, else network
 *  - other /api/*                -> passed through (never cached)
 *  - anything with /_next/       -> passed through (HMR + hashed assets)
 *  - cross-origin                -> passed through
 *  - navigations                 -> network-first, cache copy at "/", offline fallback
 *  - static shell assets         -> cache-first
 *  - everything else             -> passed through
 */
const VERSION = "music-v2"
const CACHE = VERSION
// Must match OFFLINE_CACHE in src/lib/offline.ts. Never deleted on activate:
// it holds the user's downloads, not the app shell.
const OFFLINE_CACHE = "music-offline-v1"

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
        Promise.all(
          keys
            .filter((k) => k !== CACHE && k !== OFFLINE_CACHE)
            .map((k) => caches.delete(k))
        )
      )
      .then(() => self.clients.claim())
  )
})

const STREAM_RE = /^\/api\/tracks\/([^/]+)\/(stream|cover)$/

/**
 * Answers a media request from the offline cache. Media elements ask for
 * byte ranges (and refuse to seek if they don't get 206s back), so a Range
 * header is honoured by slicing the cached Blob — which is lazy, so this
 * never copies a whole track into memory.
 */
async function serveOffline(req, cachedKey) {
  const cache = await caches.open(OFFLINE_CACHE)
  const hit = await cache.match(cachedKey)
  if (!hit) return null

  const type = hit.headers.get("Content-Type") || "application/octet-stream"
  const blob = await hit.blob()
  const size = blob.size
  const range = req.headers.get("range")

  const baseHeaders = {
    "Content-Type": type,
    "Accept-Ranges": "bytes",
    "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff",
  }

  if (!range) {
    return new Response(blob, {
      status: 200,
      headers: { ...baseHeaders, "Content-Length": String(size) },
    })
  }

  const m = /^bytes=(\d*)-(\d*)$/.exec(range.trim())
  if (!m) {
    return new Response(null, {
      status: 416,
      headers: { ...baseHeaders, "Content-Range": `bytes */${size}` },
    })
  }
  let start = m[1] === "" ? NaN : Number(m[1])
  let end = m[2] === "" ? NaN : Number(m[2])
  if (Number.isNaN(start)) {
    // suffix range: last N bytes
    start = Math.max(0, size - end)
    end = size - 1
  } else if (Number.isNaN(end) || end >= size) {
    end = size - 1
  }
  if (start > end || start >= size) {
    return new Response(null, {
      status: 416,
      headers: { ...baseHeaders, "Content-Range": `bytes */${size}` },
    })
  }
  return new Response(blob.slice(start, end + 1, type), {
    status: 206,
    headers: {
      ...baseHeaders,
      "Content-Range": `bytes ${start}-${end}/${size}`,
      "Content-Length": String(end - start + 1),
    },
  })
}

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

  // Downloaded tracks play from the device even when the server is fine —
  // instant start, no data used. The downloader itself sets offline=bypass
  // so re-downloading reads the server, not the cache.
  const media = STREAM_RE.exec(url.pathname)
  if (media && url.searchParams.get("offline") !== "bypass") {
    const key = media[2] === "stream" ? `/offline/audio/${media[1]}` : `/offline/cover/${media[1]}`
    event.respondWith(
      serveOffline(req, key)
        .catch(() => null)
        .then((res) => res || fetch(req))
    )
    return
  }

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
