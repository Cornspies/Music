// Token-aware fetch client for the Go music backend.
//
// Requests go to this app's own origin and are proxied to the backend by the
// rewrite in next.config.ts. The port below is only used for the legacy
// XTransformPort gateway parameter, and is configurable so it can match
// MUSIC_API_PORT. NEXT_PUBLIC_ is required: it is inlined into the browser
// bundle at build time, so changing it needs a rebuild.

import { gw } from "@/lib/gateway"
import type {
  AdminSettings,
  AdminUser,
  HistoryResponse,
  Invite,
  MetadataCandidatesResponse,
  PlaybackState,
  Track,
  User,
  Waveform,
  Lyrics,
  DiscoverSource,
  DiscoverSearch,
  DuplicateGroup,
  PlaylistImport,
  AppRelease,
  TrackChanges,
  TrackShare,
  SharedTrack,
  MetadataQueueStatus,
  SilenceInfo,
  MetadataCandidate,
} from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { currentMediaToken } from "@/stores/media-token"

// Re-exported so existing call sites can keep importing these from "@/lib/api".
export { gw }

/** Header the server uses to hand back a renewed session token. */
const REFRESHED_TOKEN_HEADER = "X-Refreshed-Token"

/** Persists a renewed token, if the response carried one. */
function noteRefreshedToken(res: Response): void {
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
      // A lyrics 404 is an answer ("nothing anywhere"), with the same body
      // shape as a hit; fetchLyrics unwraps it.
      if (res.status === 404 && (data as { source?: unknown })?.source === "none") {
        throw new LyricsMissing(data as Lyrics)
      }
      const msg = (data as { error?: unknown })?.error
      if (typeof msg === "string" && msg) message = msg
    } catch (e) {
      if (e instanceof LyricsMissing) throw e
      // ignore JSON parse errors on error responses
    }
    throw new Error(message)
  }
  // 204: nothing to return; 304: the caller's If-None-Match still holds.
  if (res.status === 204 || res.status === 304) return undefined as T
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
function browserCanPlay(track: Pick<Track, "mimeType" | "hasCompatible">): boolean {
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

/** Adds a held upload (one that looked like a duplicate) after all. */
export function confirmPendingUpload(pendingId: string): Promise<Track> {
  return api<Track>(`/api/uploads/pending/${pendingId}`, { method: "POST" })
}

/** Drops a held upload the person decided not to add. */
export function discardPendingUpload(pendingId: string): Promise<void> {
  return api<void>(`/api/uploads/pending/${pendingId}`, { method: "DELETE" })
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

/** Queues a fresh online lookup for a track that's already in the library. */
export function requestMetadataLookup(trackId: string): Promise<{ queued: boolean; position: number }> {
  return api(`/api/tracks/${trackId}/metadata/lookup`, { method: "POST" })
}

// ---- trimming ---------------------------------------------------------------

export function fetchSilence(trackId: string): Promise<SilenceInfo> {
  return api<SilenceInfo>(`/api/tracks/${trackId}/silence`)
}

/** Keeps the audio between start and end (seconds); the original is kept for restore. */
/** Cuts the audio to [start, end]; shiftLyrics moves synced lyrics along with the start cut. */
export function trimTrack(trackId: string, start: number, end: number, shiftLyrics = true): Promise<Track> {
  return api<Track>(`/api/tracks/${trackId}/trim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ start, end, shiftLyrics }),
  }).then((t) => {
    forgetAudioDerived(trackId)
    return t
  })
}

export function restoreTrack(trackId: string): Promise<Track> {
  return api<Track>(`/api/tracks/${trackId}/trim/restore`, { method: "POST" }).then((t) => {
    forgetAudioDerived(trackId)
    return t
  })
}

/** The waveform and lyrics timing cached for a track are stale once its audio changed. */
function forgetAudioDerived(trackId: string) {
  for (const key of waveformCache.keys()) {
    if (key.startsWith(`${trackId}:`)) waveformCache.delete(key)
  }
  lyricsCache.delete(trackId)
}

/** Adds one YouTube video's details as a candidate for the track. */
export function addYouTubeCandidate(trackId: string, url: string): Promise<MetadataCandidate> {
  return api<MetadataCandidate>(`/api/tracks/${trackId}/metadata/youtube`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  })
}

export function fetchMetadataQueue(): Promise<MetadataQueueStatus> {
  return api<MetadataQueueStatus>("/api/metadata/queue")
}

/** Apply a user-chosen metadata match, returning the updated track. */
// ---- playback position ("continue listening") ------------------------------

/** The saved position, or null when there is nothing to resume (204). */
/**
 * Where the user left off, or null when there is nothing to resume. With
 * `knownUpdatedAt` (the stamp of the state this client already has) the
 * server answers null when nothing changed since, and the state only when
 * another device saved after it.
 */
export async function fetchPlayback(knownUpdatedAt?: string): Promise<PlaybackState | null> {
  const headers: Record<string, string> = knownUpdatedAt ? { "If-None-Match": `"${knownUpdatedAt}"` } : {}
  return (await api<PlaybackState | undefined>("/api/playback", { headers })) ?? null
}

/**
 * Records where the user is. `queue` (track IDs) is optional: leave it out for
 * the frequent position-only saves and the server keeps the one it has.
 * `keepalive` lets the request outlive a page that is being closed. Resolves
 * with the stamp the save got, which fetchPlayback compares against.
 */
export function savePlayback(
  payload: { trackId: string; position: number; queue?: string[] },
  keepalive = false
): Promise<{ updatedAt: string } | undefined> {
  return api<{ updatedAt: string } | undefined>("/api/playback", {
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

// ---- account ----------------------------------------------------------------

/**
 * POST for actions confirmed with the current password. Unlike api(), a 401
 * here means "wrong password" rather than "session expired", so it must not
 * sign anyone out — it is just an error to show next to the field.
 */
async function postConfirmed<T>(path: string, body: unknown): Promise<T | undefined> {
  let res: Response
  try {
    res = await fetch(gw(path), {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${currentToken() ?? ""}` },
      body: JSON.stringify(body),
    })
  } catch {
    throw new Error("Network error — could not reach the server.")
  }
  if (!res.ok) {
    let message = `Request failed (${res.status})`
    try {
      const data = (await res.json()) as { error?: string }
      if (data.error) message = data.error
    } catch {
      // not JSON
    }
    throw new Error(message)
  }
  if (res.status === 204) return undefined
  return (await res.json()) as T
}

/** Changes the sign-in name; answers a fresh token carrying it. */
export async function renameAccount(username: string, password: string): Promise<{ token: string; user: User }> {
  const out = await postConfirmed<{ token: string; user: User }>("/api/auth/username", { username, password })
  if (!out) throw new Error("The server didn't answer with the new name.")
  return out
}

/** Deletes the account. Uploaded tracks stay in the library. */
export async function deleteAccount(password: string): Promise<void> {
  await postConfirmed<void>("/api/auth/delete-account", { password })
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

/** Tracks changed since `since` (RFC 3339), for catching up without a full reload. */
export function fetchTrackChanges(since: string): Promise<TrackChanges> {
  return api<TrackChanges>(`/api/tracks/changes?since=${encodeURIComponent(since)}`)
}

// ---- the Android app ---------------------------------------------------------

/** Android builds this server offers, newest first; empty when there are none. */
export function fetchAppReleases(): Promise<AppRelease[]> {
  return api<AppRelease[]>("/api/app/releases")
}

/** Download URL for one build (a plain link, so it carries the media token). */
export function appReleaseUrl(fileName: string): string {
  return gw(`/api/app/releases/${encodeURIComponent(fileName)}`) + tokenQuery()
}

// ---- share links -------------------------------------------------------------

/** The caller's share link for a track, or null when it isn't shared. */
export async function fetchTrackShare(trackId: string): Promise<TrackShare | null> {
  try {
    return await api<TrackShare>(`/api/tracks/${trackId}/share`)
  } catch {
    return null
  }
}

/** Creates the link (or returns the existing one). */
export function createTrackShare(trackId: string, expiresInDays = 0): Promise<TrackShare> {
  return api<TrackShare>(`/api/tracks/${trackId}/share`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expiresInDays }),
  })
}

/** Revokes the link; it stops working at once. */
export function deleteTrackShare(trackId: string): Promise<void> {
  return api<void>(`/api/tracks/${trackId}/share`, { method: "DELETE" })
}

/** The track behind a share token — no session needed. */
export function fetchSharedTrack(token: string): Promise<SharedTrack> {
  return api<SharedTrack>(`/api/share/${encodeURIComponent(token)}`)
}

/** For someone signed in: the track behind a share link, to play in the app. */
export function fetchSharedTrackForMember(token: string): Promise<Track> {
  return api<Track>(`/api/share/${encodeURIComponent(token)}/track`)
}

/** Audio and artwork for a shared track; the token is the only credential. */
export function sharedStreamUrl(token: string): string {
  return gw(`/api/share/${encodeURIComponent(token)}/stream`)
}

export function sharedCoverUrl(token: string): string {
  return gw(`/api/share/${encodeURIComponent(token)}/cover`)
}

/** Groups of library tracks that look like the same song. */
export function fetchDuplicates(): Promise<DuplicateGroup[]> {
  return api<DuplicateGroup[]>("/api/library/duplicates")
}

// ---- discover ---------------------------------------------------------------

export const discover = {
  sources: () => api<DiscoverSource[]>("/api/discover/sources"),
  /** Starts importing a playlist link; answers the job (an already running one for the same link is returned instead). */
  startImport: (source: string, url: string) =>
    api<PlaylistImport>("/api/discover/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source, url }),
    }),
  imports: () => api<PlaylistImport[]>("/api/discover/imports"),
  cancelImport: (id: string) => api<void>(`/api/discover/imports/${id}`, { method: "DELETE" }),
  search: (source: string, q: string, page = 1, signal?: AbortSignal) =>
    api<DiscoverSearch>(
      `/api/discover/search?source=${encodeURIComponent(source)}&q=${encodeURIComponent(q)}&page=${page}`,
      { signal }
    ),
  /** Result thumbnail, proxied through the server (see handleDiscoverArtwork). */
  artworkUrl: (source: string, u: string) =>
    gw(`/api/discover/artwork?source=${encodeURIComponent(source)}&u=${encodeURIComponent(u)}`) + tokenQuery(),
  /** Downloads the track into the library; the server answers with the Track. */
  add: (source: string, id: string) =>
    api<Track>("/api/discover/add", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source, id }),
    }),
}

// ---- lyrics -----------------------------------------------------------------

class LyricsMissing extends Error {
  constructor(readonly lyrics: Lyrics) {
    super("no lyrics")
  }
}

const lyricsCache = new Map<string, Promise<Lyrics>>()

/**
 * Lyrics for a track. A 404 carries a body (source "none") and is returned
 * as a value rather than thrown, so "no lyrics" needs no special casing.
 */
export function fetchLyrics(trackId: string): Promise<Lyrics> {
  let cached = lyricsCache.get(trackId)
  if (!cached) {
    cached = api<Lyrics>(`/api/tracks/${trackId}/lyrics`)
      .catch((e: Error) => {
        // api() turns the 404 into an error; recover its body as the value.
        if (e instanceof LyricsMissing) return e.lyrics
        throw e
      })
      .then((l) => {
        // A miss isn't kept: it may just be the lyrics provider having been
        // down, and the server re-asks on the next request anyway.
        if (l.source === "none") lyricsCache.delete(trackId)
        return l
      })
      .catch((e) => {
        lyricsCache.delete(trackId)
        throw e
      })
    lyricsCache.set(trackId, cached)
  }
  return cached
}

/** Replaces the lyrics (LRC time tags make them synced); "" clears them. */
export async function saveLyrics(trackId: string, text: string): Promise<Lyrics | null> {
  lyricsCache.delete(trackId)
  if (text.trim() === "") {
    await api<void>(`/api/tracks/${trackId}/lyrics`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: "" }),
    })
    return null
  }
  return api<Lyrics>(`/api/tracks/${trackId}/lyrics`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  })
}

// ---- waveform ---------------------------------------------------------------

const waveformCache = new Map<string, Promise<number[] | null>>()

/**
 * Peaks for the seek bar, or null when the track hasn't been analysed.
 *
 * The peaks describe the audio, which can change under the same id (a trim
 * re-analyses the file), so they are cached against the track's updatedAt:
 * a changed track is simply a different entry, and the stale one ages out
 * with the rest when the page goes away.
 */
export function fetchWaveform(trackId: string, version = ""): Promise<number[] | null> {
  const key = `${trackId}:${version}`
  let cached = waveformCache.get(key)
  if (!cached) {
    cached = api<Waveform>(`/api/tracks/${trackId}/waveform`)
      .then((w) => w.peaks)
      .catch(() => {
        waveformCache.delete(key)
        return null
      })
    waveformCache.set(key, cached)
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
  invites: () => api<Invite[]>("/api/admin/invites"),
  createInvite: (body: { note?: string; expiresHours?: number }) =>
    api<Invite>("/api/admin/invites", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
  revokeInvite: (code: string) =>
    api<Invite>(`/api/admin/invites/${encodeURIComponent(code)}/revoke`, { method: "POST" }),
  deleteInvite: (code: string) =>
    api<void>(`/api/admin/invites/${encodeURIComponent(code)}`, { method: "DELETE" }),
}

/** The shareable URL for an invite code, on this origin. */
export function inviteLink(code: string): string {
  const origin = typeof window !== "undefined" ? window.location.origin : ""
  return `${origin}/register?invite=${encodeURIComponent(code)}`
}
