// Offline downloads for the PWA.
//
// A downloaded track is three entries in one Cache Storage bucket, keyed by
// token-free paths the service worker can look up without knowing anything
// about sessions:
//
//   /offline/audio/<id>   the audio bytes, with their Content-Type
//   /offline/cover/<id>   the cover art (optional)
//   /offline/meta/<id>    the Track JSON, so the list survives a cold start
//
// The service worker (public/sw.js) answers /api/tracks/<id>/stream and
// /cover from here when an entry exists — with Range support, so seeking
// works — and lets everything else go to the network as usual. Nothing here
// stores a token: the fetches below send the session in a header and the
// cached responses are stripped of anything but the bytes.

import type { Track } from "@/lib/types"
import { gw } from "@/lib/gateway"
import { useAuthStore } from "@/stores/auth"

export const OFFLINE_CACHE = "music-offline-v1"

const audioKey = (id: string) => `/offline/audio/${id}`
const coverKey = (id: string) => `/offline/cover/${id}`
const metaKey = (id: string) => `/offline/meta/${id}`

export function offlineSupported(): boolean {
  return typeof window !== "undefined" && "caches" in window
}

async function openCache(): Promise<Cache | null> {
  if (!offlineSupported()) return null
  try {
    return await caches.open(OFFLINE_CACHE)
  } catch {
    return null
  }
}

/** Every track stored on this device, newest download first. */
export async function listOfflineTracks(): Promise<Track[]> {
  const cache = await openCache()
  if (!cache) return []
  const keys = await cache.keys()
  const tracks: Track[] = []
  for (const req of keys) {
    const path = new URL(req.url).pathname
    if (!path.startsWith("/offline/meta/")) continue
    const res = await cache.match(req)
    if (!res) continue
    try {
      tracks.push((await res.json()) as Track)
    } catch {
      // a corrupt entry is simply not listed
    }
  }
  return tracks.sort((a, b) => (b.updatedAt ?? "").localeCompare(a.updatedAt ?? ""))
}

/** Bytes of audio held offline (covers are negligible). */
export async function offlineBytesUsed(): Promise<number> {
  const cache = await openCache()
  if (!cache) return 0
  let total = 0
  for (const req of await cache.keys()) {
    if (!new URL(req.url).pathname.startsWith("/offline/audio/")) continue
    const res = await cache.match(req)
    const len = res?.headers.get("Content-Length")
    if (len) total += Number(len)
  }
  return total
}

export type DownloadProgress = (loaded: number, total: number) => void

/**
 * Downloads a track's audio (and cover) into the offline cache. Picks the
 * compatible copy when the browser can't decode the original, same as
 * streaming does. Progress is reported as bytes when the server says how
 * many there are.
 */
export async function downloadTrackOffline(
  track: Track,
  onProgress?: DownloadProgress,
  signal?: AbortSignal
): Promise<void> {
  const cache = await openCache()
  if (!cache) throw new Error("Offline storage isn't available in this browser.")
  const token = useAuthStore.getState().token
  if (!token) throw new Error("Not signed in.")
  const headers = { Authorization: `Bearer ${token}` }

  const probe = typeof document !== "undefined" ? document.createElement("audio") : null
  const useCompatible = Boolean(track.hasCompatible && probe && probe.canPlayType(track.mimeType) === "")
  const path = `/api/tracks/${track.id}/stream${useCompatible ? "?version=compatible" : ""}`

  // The stream endpoint is also the offline-serving path in the SW, so ask
  // it to skip the cache for this one fetch — otherwise re-downloading would
  // read back what's already stored.
  const res = await fetch(gw(path) + "&offline=bypass", { headers, signal })
  if (!res.ok || !res.body) throw new Error(`Download failed (${res.status})`)
  const contentType = res.headers.get("Content-Type") ?? (useCompatible ? "audio/mp4" : track.mimeType)
  const total = Number(res.headers.get("Content-Length") ?? 0)

  const reader = res.body.getReader()
  const chunks: BlobPart[] = []
  let loaded = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    chunks.push(value)
    loaded += value.byteLength
    onProgress?.(loaded, total)
  }
  const blob = new Blob(chunks, { type: contentType })
  await cache.put(
    audioKey(track.id),
    new Response(blob, {
      headers: { "Content-Type": contentType, "Content-Length": String(blob.size) },
    })
  )

  if (track.hasCover) {
    try {
      const cover = await fetch(gw(`/api/tracks/${track.id}/cover`) + "&offline=bypass", { headers, signal })
      if (cover.ok) {
        const b = await cover.blob()
        await cache.put(
          coverKey(track.id),
          new Response(b, { headers: { "Content-Type": b.type || "image/jpeg", "Content-Length": String(b.size) } })
        )
      }
    } catch {
      // cover is optional
    }
  }

  await cache.put(
    metaKey(track.id),
    new Response(JSON.stringify({ ...track, updatedAt: new Date().toISOString() }), {
      headers: { "Content-Type": "application/json" },
    })
  )
}

export async function removeTrackOffline(id: string): Promise<void> {
  const cache = await openCache()
  if (!cache) return
  await Promise.all([cache.delete(audioKey(id)), cache.delete(coverKey(id)), cache.delete(metaKey(id))])
}

export async function clearOffline(): Promise<void> {
  if (!offlineSupported()) return
  try {
    await caches.delete(OFFLINE_CACHE)
  } catch {
    // ignore
  }
}

/**
 * Asks the browser not to evict the offline cache under storage pressure.
 * Best-effort; most browsers grant it to installed PWAs and to sites the
 * user interacts with a lot.
 */
export async function requestPersistentStorage(): Promise<boolean> {
  try {
    if (navigator.storage?.persist) return await navigator.storage.persist()
  } catch {
    // ignore
  }
  return false
}
