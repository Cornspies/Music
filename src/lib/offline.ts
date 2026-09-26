import type { Track } from "@/lib/types"
import { gw } from "@/lib/gateway"
import { useAuthStore } from "@/stores/auth"

const OFFLINE_CACHE = "music-offline-v1"

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
    } catch {}
  }
  return tracks.sort((a, b) => (b.updatedAt ?? "").localeCompare(a.updatedAt ?? ""))
}

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
    } catch {}
  }

  await cache.put(
    metaKey(track.id),
    new Response(JSON.stringify({ ...track, updatedAt: new Date().toISOString() }), {
      headers: { "Content-Type": "application/json" },
    })
  )
}

let metaWrites: Promise<unknown> = Promise.resolve()

function inOrder<T>(task: () => Promise<T>): Promise<T> {
  const run = metaWrites.then(task)
  metaWrites = run.catch(() => {})
  return run
}

export function updateOfflineTrack(track: Track): Promise<void> {
  return inOrder(async () => {
    const cache = await openCache()
    if (!cache || !(await cache.match(metaKey(track.id))) || !(await cache.match(audioKey(track.id)))) return
    await cache.put(
      metaKey(track.id),
      new Response(JSON.stringify(track), { headers: { "Content-Type": "application/json" } })
    )
  })
}

export function removeTrackOffline(id: string): Promise<void> {
  return inOrder(async () => {
    const cache = await openCache()
    if (!cache) return
    await Promise.all([cache.delete(audioKey(id)), cache.delete(coverKey(id)), cache.delete(metaKey(id))])
  })
}

export async function clearOffline(): Promise<void> {
  if (!offlineSupported()) return
  try {
    await caches.delete(OFFLINE_CACHE)
  } catch {}
}

export async function requestPersistentStorage(): Promise<boolean> {
  try {
    if (navigator.storage?.persist) return await navigator.storage.persist()
  } catch {}
  return false
}
