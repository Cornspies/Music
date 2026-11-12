const VERSION = "music-v3"
const CACHE = VERSION
const OFFLINE_CACHE = "music-offline-v1"
const COVER_CACHE = "music-covers-v1"
const COVER_MAX_AGE_MS = 24 * 60 * 60 * 1000
const COVER_MAX_ENTRIES = 2000
const COVER_STAMP = "X-Cached-At"

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
            .filter((k) => k !== CACHE && k !== OFFLINE_CACHE && k !== COVER_CACHE)
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

let coverPuts = 0

function coverKey(id, size, version) {
  return `/covers/${id}/${size || "full"}/${version || "-"}`
}

async function trimCovers(cache) {
  const keys = await cache.keys()
  for (let i = 0; i < keys.length - COVER_MAX_ENTRIES; i++) await cache.delete(keys[i])
}

async function storeCover(cache, id, size, version, res) {
  const body = await res.blob()
  await cache.put(
    coverKey(id, size, version),
    new Response(body, {
      headers: {
        "Content-Type": res.headers.get("Content-Type") || "image/jpeg",
        "X-Content-Type-Options": "nosniff",
        [COVER_STAMP]: String(Date.now()),
      },
    })
  )
  if (version) await cache.delete(coverKey(id, size, ""))
  if (++coverPuts % 50 === 0) await trimCovers(cache)
}

async function serveCover(event, req, url, id) {
  const size = url.searchParams.get("size") || ""
  const version = url.searchParams.get("v") || ""
  const cache = await caches.open(COVER_CACHE)
  const key = coverKey(id, size, version)
  const hit = await cache.match(key)
  if (hit && (version || Date.now() - Number(hit.headers.get(COVER_STAMP)) < COVER_MAX_AGE_MS)) return hit

  const offline = () => serveOffline(req, `/offline/cover/${id}`).catch(() => null)
  if (!size || self.navigator.onLine === false) {
    const local = await offline()
    if (local) return local
  }
  try {
    const res = await fetch(req)
    if (res.status === 200) event.waitUntil(storeCover(cache, id, size, version, res.clone()).catch(() => {}))
    else if (res.status === 404 && hit) event.waitUntil(cache.delete(key))
    if (res.ok || res.status === 404 || !hit) return res
    return hit
  } catch (err) {
    if (hit) return hit
    const local = await offline()
    if (local) return local
    throw err
  }
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
    if (media[2] === "cover") {
      event.respondWith(serveCover(event, req, url, media[1]))
      return
    }
    event.respondWith(
      serveOffline(req, `/offline/audio/${media[1]}`)
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
