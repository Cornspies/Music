const VERSION = "music-v3"
const CACHE = VERSION
const OFFLINE_CACHE = "music-offline-v1"

// Pages that aren't the app. Every other page load is the app itself and is
// kept as its offline copy; these must never take its place.
const STANDALONE_PAGES = ["/privacy"]

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
      Promise.all(
        PRECACHE.map((url) =>
          cache.add(url).catch(() => {})
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

  if (req.method !== "GET") return

  let url
  try {
    url = new URL(req.url)
  } catch {
    return
  }

  if (url.origin !== self.location.origin) return

  if (req.mode === "websocket") return

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

  if (url.pathname.startsWith("/api/")) return

  if (url.pathname.includes("/_next/")) return

  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req)
        .then((response) => {
          if (response && response.ok && !STANDALONE_PAGES.includes(url.pathname)) {
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
})
