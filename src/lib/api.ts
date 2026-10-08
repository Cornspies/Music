import type {
  AdminSettings,
  ListenInvite,
  ListenState,
  AdminUser,
  HistoryResponse,
  Invite,
  MetadataCandidatesResponse,
  PlaybackState,
  Role,
  Track,
  User,
  Waveform,
  Lyrics,
  DiscoverSource,
  DiscoverSearch,
  DuplicateGroup,
  PlaylistImport,
  AdminShare,
  AppRelease,
  TrackChanges,
  TrackShare,
  SharedTrack,
  MetadataQueueStatus,
  SilenceInfo,
  MetadataCandidate,
  ActivityStatus,
  AttentionItem,
  BulkApplyResult,
  BulkApplyStatus,
  FileTagDiff,
  TagChange,
} from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { currentMediaToken } from "@/stores/media-token"
import { apiUrl, publicUrl } from "@/lib/desktop"


const REFRESHED_TOKEN_HEADER = "X-Refreshed-Token"

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly data: unknown,
  ) {
    super(message)
  }
}

function noteRefreshedToken(res: Response): void {
  const fresh = res.headers.get(REFRESHED_TOKEN_HEADER)
  if (fresh && fresh !== useAuthStore.getState().token) {
    useAuthStore.setState({ token: fresh })
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  const token = useAuthStore.getState().token
  if (token) headers.set("Authorization", `Bearer ${token}`)
  let res: Response
  try {
    res = await fetch(apiUrl(path), { ...init, headers })
  } catch (e) {
    const err = e as Error
    if (err?.name === "AbortError") throw err
    throw new Error("Network error — could not reach the server.")
  }
  noteRefreshedToken(res)

  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error("Session expired. Please sign in again.")
  }
  if (!res.ok) {
    let message = `Request failed (${res.status})`
    let data: unknown = null
    try {
      data = await res.json()
      if (res.status === 404 && (data as { source?: unknown })?.source === "none") {
        throw new LyricsMissing(data as Lyrics)
      }
      const msg = (data as { error?: unknown })?.error
      if (typeof msg === "string" && msg) message = msg
    } catch (e) {
      if (e instanceof LyricsMissing) throw e
    }
    throw new ApiError(message, res.status, data)
  }
  if (res.status === 204 || res.status === 304) return undefined as T
  return (await res.json()) as T
}

function withToken(path: string): string {
  return `${apiUrl(path)}${path.includes("?") ? "&" : "?"}token=${encodeURIComponent(currentMediaToken())}`
}

export function streamUrl(track: Pick<Track, "id" | "mimeType" | "hasCompatible">): string {
  const version = browserCanPlay(track) ? "" : "&version=compatible"
  return withToken(`/api/tracks/${track.id}/stream`) + version
}

export function nativeStreamUrl(track: Pick<Track, "id">): string {
  return withToken(`/api/tracks/${track.id}/stream`) + "&version=compatible"
}

function browserCanPlay(track: Pick<Track, "mimeType" | "hasCompatible">): boolean {
  if (!track.hasCompatible) return true
  if (typeof document === "undefined") return true
  const probe = document.createElement("audio")
  return probe.canPlayType(track.mimeType) !== ""
}

export type CoverSize = 32 | 128 | 256 | 512

export function coverUrl(track: { id: string }, size?: CoverSize): string {
  return withToken(`/api/tracks/${track.id}/cover${size ? `?size=${size}` : ""}`)
}

export function candidateArtworkUrl(trackId: string, candidateId: string): string {
  return withToken(`/api/tracks/${trackId}/metadata/candidates/${candidateId}/artwork`)
}

export function downloadUrl(
  track: Pick<Track, "id">,
  version: "original" | "compatible" = "original",
): string {
  const suffix = version === "compatible" ? "&version=compatible" : ""
  return withToken(`/api/tracks/${track.id}/download`) + suffix
}

export function uploadUrl(): string {
  return apiUrl("/api/tracks")
}

export function confirmPendingUpload(pendingId: string): Promise<Track> {
  return api<Track>(`/api/uploads/pending/${pendingId}`, { method: "POST" })
}

export function discardPendingUpload(pendingId: string): Promise<void> {
  return api<void>(`/api/uploads/pending/${pendingId}`, { method: "DELETE" })
}

export function avatarUrl(userId: string): string {
  return withToken(`/api/users/${userId}/avatar`)
}

export interface AvatarCrop {
  x: number
  y: number
  size: number
}

export async function uploadAvatar(file: File, crop?: AvatarCrop): Promise<User> {
  const form = new FormData()
  form.append("file", file)
  if (crop) {
    form.append("cropX", String(crop.x))
    form.append("cropY", String(crop.y))
    form.append("cropSize", String(crop.size))
  }
  return api<User>("/api/auth/avatar", { method: "POST", body: form })
}

export function removeAvatar(): Promise<User> {
  return api<User>("/api/auth/avatar", { method: "DELETE" })
}

export function currentToken(): string | null {
  return useAuthStore.getState().token
}

export function fetchMetadataCandidates(
  trackId: string,
  signal?: AbortSignal
): Promise<MetadataCandidatesResponse> {
  return api<MetadataCandidatesResponse>(`/api/tracks/${trackId}/metadata/candidates`, { signal })
}

export function requestMetadataLookup(trackId: string): Promise<{ queued: boolean; position: number; paused: boolean }> {
  return api(`/api/tracks/${trackId}/metadata/lookup`, { method: "POST" })
}

export function fetchSilence(trackId: string): Promise<SilenceInfo> {
  return api<SilenceInfo>(`/api/tracks/${trackId}/silence`)
}

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

function forgetAudioDerived(trackId: string) {
  for (const key of waveformCache.keys()) {
    if (key.startsWith(`${trackId}:`)) waveformCache.delete(key)
  }
  lyricsCache.delete(trackId)
}

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

export async function fetchPlayback(knownUpdatedAt?: string): Promise<PlaybackState | null> {
  const headers: Record<string, string> = knownUpdatedAt ? { "If-None-Match": `"${knownUpdatedAt}"` } : {}
  return (await api<PlaybackState | undefined>("/api/playback", { headers })) ?? null
}

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

async function postConfirmed<T>(path: string, body: unknown): Promise<T | undefined> {
  let res: Response
  try {
    res = await fetch(apiUrl(path), {
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
    } catch {}
    throw new Error(message)
  }
  if (res.status === 204) return undefined
  return (await res.json()) as T
}

export async function renameAccount(username: string, password: string): Promise<{ token: string; user: User }> {
  const out = await postConfirmed<{ token: string; user: User }>("/api/auth/username", { username, password })
  if (!out) throw new Error("The server didn't answer with the new name.")
  return out
}

export async function deleteAccount(password: string): Promise<void> {
  await postConfirmed<void>("/api/auth/delete-account", { password })
}

export function logoutEverywhere(): Promise<void> {
  return api<void>("/api/auth/logout-all", { method: "POST" })
}

export function fetchHistory(opts: { limit?: number; distinct?: boolean; since?: string } = {}): Promise<HistoryResponse> {
  const params = new URLSearchParams()
  if (opts.limit) params.set("limit", String(opts.limit))
  if (opts.distinct) params.set("distinct", "1")
  if (opts.since) params.set("since", opts.since)
  const qs = params.toString()
  return api<HistoryResponse>(`/api/history${qs ? `?${qs}` : ""}`)
}

export function fetchTrackChanges(since: string): Promise<TrackChanges> {
  return api<TrackChanges>(`/api/tracks/changes?since=${encodeURIComponent(since)}`)
}

export type ReleasePlatform = "android" | "windows" | "appimage" | "deb" | "rpm"

export function fetchAppReleases(platform: ReleasePlatform): Promise<AppRelease[]> {
  return api<AppRelease[]>(`/api/app/releases?platform=${platform}`)
}

export function fetchLatestAppRelease(
  platform: string,
  current: string
): Promise<{ latest: AppRelease | null; updateAvailable: boolean }> {
  const query = `platform=${encodeURIComponent(platform)}&current=${encodeURIComponent(current)}`
  return api(`/api/app/releases/latest?${query}`)
}

export function appReleaseUrl(fileName: string): string {
  return withToken(`/api/app/releases/${encodeURIComponent(fileName)}`)
}

export async function fetchTrackShare(trackId: string): Promise<TrackShare | null> {
  try {
    return await api<TrackShare>(`/api/tracks/${trackId}/share`)
  } catch {
    return null
  }
}

export function createTrackShare(trackId: string, expiresInDays = 0): Promise<TrackShare> {
  return api<TrackShare>(`/api/tracks/${trackId}/share`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ expiresInDays }),
  })
}

export function deleteTrackShare(trackId: string): Promise<void> {
  return api<void>(`/api/tracks/${trackId}/share`, { method: "DELETE" })
}

export function fetchSharedTrack(token: string): Promise<SharedTrack> {
  return api<SharedTrack>(`/api/share/${encodeURIComponent(token)}`)
}

export function fetchSharedTrackForMember(token: string): Promise<Track> {
  return api<Track>(`/api/share/${encodeURIComponent(token)}/track`)
}

export function sharedStreamUrl(token: string): string {
  return apiUrl(`/api/share/${encodeURIComponent(token)}/stream`)
}

export function sharedCoverUrl(token: string): string {
  return apiUrl(`/api/share/${encodeURIComponent(token)}/cover`)
}

export function fetchDuplicates(): Promise<DuplicateGroup[]> {
  return api<DuplicateGroup[]>("/api/library/duplicates")
}

export function fetchFileTagChanges(trackId: string): Promise<{ changes: TagChange[] }> {
  return api(`/api/tracks/${trackId}/metadata/file-tags`)
}

export function restoreTrackTags(trackId: string, fields: TagChange["field"][]): Promise<{ track: Track; restored: string[] }> {
  return api(`/api/tracks/${trackId}/metadata/restore`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ fields }),
  })
}

export function fetchLibraryFileTags(): Promise<FileTagDiff[]> {
  return api<FileTagDiff[]>("/api/library/file-tags")
}

export function restoreLibraryTags(trackIds: string[]): Promise<{ restored: number }> {
  return api("/api/library/file-tags/restore", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ trackIds }),
  })
}

export function fetchAttention(hidden = false): Promise<AttentionItem[]> {
  return api<AttentionItem[]>(`/api/library/attention${hidden ? "?hidden=1" : ""}`)
}

export function dismissAttention(trackId: string): Promise<void> {
  return api<void>(`/api/tracks/${trackId}/attention/dismiss`, { method: "POST" })
}

export function restoreAttention(trackId: string): Promise<void> {
  return api<void>(`/api/tracks/${trackId}/attention/restore`, { method: "POST" })
}

export function applyConfidentMatches(trackIds: string[]): Promise<BulkApplyResult> {
  return api<BulkApplyResult>("/api/library/attention/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ trackIds }),
  })
}

export function fetchApplyStatus(): Promise<BulkApplyStatus> {
  return api<BulkApplyStatus>("/api/library/attention/apply")
}

export const discover = {
  sources: () => api<DiscoverSource[]>("/api/discover/sources"),
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
  artworkUrl: (source: string, u: string) =>
    withToken(`/api/discover/artwork?source=${encodeURIComponent(source)}&u=${encodeURIComponent(u)}`),
  add: (source: string, id: string) =>
    api<Track>("/api/discover/add", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source, id }),
    }),
}

class LyricsMissing extends Error {
  constructor(readonly lyrics: Lyrics) {
    super("no lyrics")
  }
}

const lyricsCache = new Map<string, Promise<Lyrics>>()

export function fetchLyrics(trackId: string): Promise<Lyrics> {
  let cached = lyricsCache.get(trackId)
  if (!cached) {
    cached = api<Lyrics>(`/api/tracks/${trackId}/lyrics`)
      .catch((e: Error) => {
        if (e instanceof LyricsMissing) return e.lyrics
        throw e
      })
      .then((l) => {
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

const waveformCache = new Map<string, Promise<number[] | null>>()

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

export const admin = {
  users: () => api<AdminUser[]>("/api/admin/users"),
  activity: () => api<ActivityStatus>("/api/admin/activity"),
  deleteUser: (id: string) => api<void>(`/api/admin/users/${id}`, { method: "DELETE" }),
  updateUser: (id: string, patch: { role?: Role; signOut?: boolean }) =>
    api<void>(`/api/admin/users/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }),
  settings: () => api<AdminSettings>("/api/admin/settings"),
  updateSettings: (patch: { registrationOpen?: boolean; metadataLookupPaused?: boolean }) =>
    api<AdminSettings>("/api/admin/settings", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }),
  shares: () => api<AdminShare[]>("/api/admin/shares"),
  revokeShare: (token: string) =>
    api<void>(`/api/admin/shares/${encodeURIComponent(token)}`, { method: "DELETE" }),
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

function jsonBody(body: unknown): RequestInit {
  return { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }
}

export const listen = {
  current: () => api<ListenState | undefined>("/api/listen"),
  create: (body: {
    capacity: number
    everyoneCanControl: boolean
    trackId?: string
    position?: number
    upcoming?: string[]
  }) => api<ListenState>("/api/listen", { method: "POST", ...jsonBody(body) }),
  invite: (code: string) => api<ListenInvite>(`/api/listen/invite/${encodeURIComponent(code)}`),
  join: (code: string) =>
    api<ListenState>(`/api/listen/invite/${encodeURIComponent(code)}/join`, { method: "POST" }),
  poll: (id: string, since: number, queue: number, signal?: AbortSignal) =>
    api<ListenState>(`/api/listen/${id}?since=${since}&queue=${queue}`, { signal }),
  control: (id: string, queue: number, body: { action: string; position?: number; entryId?: string }) =>
    api<ListenState>(`/api/listen/${id}/control?queue=${queue}`, { method: "POST", ...jsonBody(body) }),
  add: (id: string, queue: number, trackIds: string[], mode: "end" | "next" | "now") =>
    api<ListenState>(`/api/listen/${id}/queue?queue=${queue}`, { method: "POST", ...jsonBody({ trackIds, mode }) }),
  remove: (id: string, queue: number, entryId: string) =>
    api<ListenState>(`/api/listen/${id}/queue/${encodeURIComponent(entryId)}?queue=${queue}`, { method: "DELETE" }),
  clear: (id: string, queue: number) => api<ListenState>(`/api/listen/${id}/queue?queue=${queue}`, { method: "DELETE" }),
  settings: (id: string, queue: number, body: { capacity?: number; everyoneCanControl?: boolean }) =>
    api<ListenState>(`/api/listen/${id}?queue=${queue}`, { method: "PATCH", ...jsonBody(body) }),
  removeMember: (id: string, queue: number, userId: string) =>
    api<ListenState>(`/api/listen/${id}/members/${encodeURIComponent(userId)}?queue=${queue}`, { method: "DELETE" }),
  leave: (id: string) => api<void>(`/api/listen/${id}/leave`, { method: "POST" }),
  end: (id: string) => api<void>(`/api/listen/${id}`, { method: "DELETE" }),
}

export function listenLink(code: string): string {
  return publicUrl(`/listen/${encodeURIComponent(code)}`)
}

export function inviteLink(code: string): string {
  return publicUrl(`/register?invite=${encodeURIComponent(code)}`)
}
