// Token-aware fetch client for the Go music backend.
//
// Requests go to this app's own origin and are proxied to the backend by the
// rewrite in next.config.ts. The port below is only used for the legacy
// XTransformPort gateway parameter, and is configurable so it can match
// MUSIC_API_PORT. NEXT_PUBLIC_ is required: it is inlined into the browser
// bundle at build time, so changing it needs a rebuild.

import { API_PORT, gw } from "@/lib/gateway"
import type {
  AdminSettings,
  AdminUser,
  HistoryResponse,
  MetadataCandidatesResponse,
  PlaybackState,
  Track,
  TrackChanges,
  User,
  Waveform,
} from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { currentMediaToken } from "@/stores/media-token"

// Re-exported so existing call sites can keep importing these from "@/lib/api".
export { API_PORT, gw }

/** Header the server uses to hand back a renewed session token. */
const REFRESHED_TOKEN_HEADER = "X-Refreshed-Token"

/** Persists a renewed token, if the response carried one. */
export function noteRefreshedToken(res: Response): void {
  const fresh = res.headers.get(REFRESHED_TOKEN_HEADER)
  if (fresh && fresh !== useAuthStore.getState().token) {
    useAuthStore.setState({ token: fresh })
  }
}

/**
 * Authenticated JSON fetch. Adds the Bearer token from the auth store,
 * parses JSON, throws Error(message) on !ok. A 401 logs the user out.
 */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  const token = useAuthStore.getState().token
  if (token) headers.set("Authorization", `Bearer ${token}`)
  let res: Response
  try {
    res = await fetch(gw(path), { ...init, headers })
  } catch (e) {
    const err = e as Error
    if (err?.name === "AbortError") throw err
    throw new Error("Network error — could not reach the server.")
  }
  // Sliding sessions: once a token is past the halfway point of its life the
  // server returns a replacement here. Storing it keeps the session rolling
  // forward, so regular use never leads to a forced sign-in.
  noteRefreshedToken(res)

  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error("Session expired. Please sign in again.")
  }
  if (!res.ok) {
    let message = `Request failed (${res.status})`
    try {
      const data: unknown = await res.json()
      const msg = (data as { error?: unknown })?.error
      if (typeof msg === "string" && msg) message = msg
    } catch {
      // ignore JSON parse errors on error responses
    }
    throw new Error(message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/**
 * Token as a URL query param (for <audio>/<img> that can't send headers).
 * This is the short-lived media token, never the session — see
 * stores/media-token.ts for why.
 */
function tokenQuery(): string {
  return `&token=${encodeURIComponent(currentMediaToken())}`
}

/** Streaming URL for the <audio> element (reads token at call time). */
export function streamUrl(track: Pick<Track, "id" | "mimeType" | "hasCompatible">): string {
  const version = browserCanPlay(track) ? "" : "&version=compatible"
  return gw(`/api/tracks/${track.id}/stream`) + tokenQuery() + version
}

/**
 * Whether this browser can decode the original upload.
 *
 * Safari can't play Ogg at all, and other browsers vary by codec, so the
 * question is asked of the browser rather than guessed from the format. When
 * the answer is no and the server made a compatible copy at upload time, the
 * player streams that instead.
 */
export function browserCanPlay(track: Pick<Track, "mimeType" | "hasCompatible">): boolean {
  if (!track.hasCompatible) return true // nothing else to offer anyway
  if (typeof document === "undefined") return true // SSR: decided on the client
  const probe = document.createElement("audio")
  // canPlayType returns "probably" | "maybe" | "" — only "" is a firm no.
  return probe.canPlayType(track.mimeType) !== ""
}

/** Cover art URL for <img> (404 when no cover — handle with onError). */
export function coverUrl(track: { id: string }): string {
  return gw(`/api/tracks/${track.id}/cover`) + tokenQuery()
}

/**
 * Thumbnail URL for a metadata suggestion, proxied through the server. 404s
 * when the candidate has no artwork or its list has expired — handle with
 * onError, same as coverUrl.
 */
export function candidateArtworkUrl(trackId: string, candidateId: string): string {
  return gw(`/api/tracks/${trackId}/metadata/candidates/${candidateId}/artwork`) + tokenQuery()
}

/** Download URL (attachment) for <a href download>. */
export function downloadUrl(
  track: Pick<Track, "id">,
  version: "original" | "compatible" = "original",
): string {
  const suffix = version === "compatible" ? "&version=compatible" : ""
  return gw(`/api/tracks/${track.id}/download`) + tokenQuery() + suffix
}

/** Upload endpoint (XHR POST with multipart). */
export function uploadUrl(): string {
  return gw("/api/tracks")
}

/** Avatar URL for <img> (404 when the user has none — handle with onError). */
export function avatarUrl(userId: string): string {
  return gw(`/api/users/${userId}/avatar`) + tokenQuery()
}

/**
 * The square of the source image to keep, as fractions of the image's own
 * width and height, so it survives whatever scaling was used for display.
 * `size` is a fraction of the shorter edge. Omit it for a centre crop.
 */
export interface AvatarCrop {
  x: number
  y: number
  size: number
}

/** Replaces the signed-in user's avatar; returns the updated profile. */
export async function uploadAvatar(file: File, crop?: AvatarCrop): Promise<User> {
  const form = new FormData()
  form.append("file", file)
  if (crop) {
    form.append("cropX", String(crop.x))
    form.append("cropY", String(crop.y))
    form.append("cropSize", String(crop.size))
  }
  // No Content-Type header: the browser must set the multipart boundary.
  return api<User>("/api/auth/avatar", { method: "POST", body: form })
}

/** Removes the signed-in user's avatar; returns the updated profile. */
export function removeAvatar(): Promise<User> {
  return api<User>("/api/auth/avatar", { method: "DELETE" })
}

/** Current bearer token (for XHR uploads). */
export function currentToken(): string | null {
  return useAuthStore.getState().token
}

/** Online metadata matches for a track, best first. */
export function fetchMetadataCandidates(
  trackId: string,
  signal?: AbortSignal
): Promise<MetadataCandidatesResponse> {
  return api<MetadataCandidatesResponse>(`/api/tracks/${trackId}/metadata/candidates`, { signal })
}

/** Apply a user-chosen metadata match, returning the updated track. */
// ---- playback position ("continue listening") ------------------------------

/** The saved position, or null when there is nothing to resume (204). */
export async function fetchPlayback(): Promise<PlaybackState | null> {
  return (await api<PlaybackState | undefined>("/api/playback")) ?? null
}

/**
 * Records where the user is. `queue` (track IDs) is optional: leave it out for
 * the frequent position-only saves and the server keeps the one it has.
 * `keepalive` lets the request outlive a page that is being closed.
 */
export function savePlayback(
  payload: { trackId: string; position: number; queue?: string[] },
  keepalive = false
): Promise<void> {
  return api<void>("/api/playback", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
    keepalive,
  })
}

export function applyMetadataCandidate(trackId: string, candidateId: string): Promise<Track> {
  return api<Track>(`/api/tracks/${trackId}/metadata/apply`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ candidateId }),
  })
}

// ---- sessions ---------------------------------------------------------------

/** Invalidates every session this account has, on every device. */
export function logoutEverywhere(): Promise<void> {
  return api<void>("/api/auth/logout-all", { method: "POST" })
}

// ---- listening history ------------------------------------------------------

export function fetchHistory(opts: { limit?: number; distinct?: boolean; since?: string } = {}): Promise<HistoryResponse> {
  const params = new URLSearchParams()
  if (opts.limit) params.set("limit", String(opts.limit))
  if (opts.distinct) params.set("distinct", "1")
  if (opts.since) params.set("since", opts.since)
  const qs = params.toString()
  return api<HistoryResponse>(`/api/history${qs ? `?${qs}` : ""}`)
}

// ---- delta sync -------------------------------------------------------------

/** Everything changed or deleted since `since` (omit for a full listing). */
export function fetchTrackChanges(since?: string): Promise<TrackChanges> {
  return api<TrackChanges>(`/api/tracks/changes${since ? `?since=${encodeURIComponent(since)}` : ""}`)
}

// ---- waveform ---------------------------------------------------------------

const waveformCache = new Map<string, Promise<number[] | null>>()

/** Peaks for the seek bar, or null when the track hasn't been analysed. */
export function fetchWaveform(trackId: string): Promise<number[] | null> {
  let cached = waveformCache.get(trackId)
  if (!cached) {
    cached = api<Waveform>(`/api/tracks/${trackId}/waveform`)
      .then((w) => w.peaks)
      .catch(() => {
        waveformCache.delete(trackId)
        return null
      })
    waveformCache.set(trackId, cached)
  }
  return cached
}

// ---- admin ------------------------------------------------------------------

export const admin = {
  users: () => api<AdminUser[]>("/api/admin/users"),
  deleteUser: (id: string) => api<void>(`/api/admin/users/${id}`, { method: "DELETE" }),
  updateUser: (id: string, patch: { role?: "user" | "admin"; signOut?: boolean }) =>
    api<void>(`/api/admin/users/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }),
  settings: () => api<AdminSettings>("/api/admin/settings"),
  updateSettings: (patch: { registrationOpen?: boolean }) =>
    api<AdminSettings>("/api/admin/settings", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }),
}
