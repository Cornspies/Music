export interface Track {
  id: string
  title: string
  artist: string
  album: string
  genre: string
  year: number
  trackNo: number
  duration: number
  mimeType: string
  fileSize: number
  hasCover: boolean
  plays: number
  isFavorite: boolean
  createdAt: string
  lastPlayedAt: string | null
  uploadedBy: Uploader
  hasCompatible: boolean
  compatibleMimeType: string
  compatibleFileSize: number
  updatedAt: string
  loudness: number | null
  hasWaveform: boolean
  source: string
  sourceId: string
  coverColor: string
  metadataStatus: string
  trimmed: boolean
}

export interface SilenceInfo {
  duration: number
  leadingEnd: number
  trailingStart: number
}

export interface Uploader {
  id: string
  username: string
  hasAvatar: boolean
}

export type Role = "user" | "admin"

export interface User {
  id: string
  username: string
  createdAt: string
  hasAvatar: boolean
  role: Role
}

export interface MediaToken {
  token: string
  expiresAt: string
}

export interface AuthResponse {
  token: string
  user: User
}

export interface Album {
  name: string
  artist: string
  trackCount: number
  totalDuration: number
  hasCover: boolean
  coverTrackId: string
}

export interface Artist {
  name: string
  trackCount: number
  totalDuration: number
}

export interface Playlist {
  id: string
  name: string
  trackCount: number
  totalDuration: number
  createdAt: string
  isPublic: boolean
  owner: Uploader
  isMine: boolean
  isPinned: boolean
}

export interface PlaylistDetail extends Playlist {
  tracks: Track[]
}

export interface Stats {
  trackCount: number
  totalDuration: number
  artistCount: number
  albumCount: number
  favoriteCount: number
  playlistCount: number
  mostPlayed: Track[]
  recentlyPlayed: Track[]
  listenedSeconds: number
  playCount: number
}

export interface PlayEvent {
  track: Track
  playedAt: string
}

export interface HistoryResponse {
  events: PlayEvent[]
  listenedSeconds: number
  playCount: number
  trackCount: number
}


export interface LyricLine {
  t: number
  line: string
}

export interface Lyrics {
  plain: string
  synced: LyricLine[] | null
  source: "embedded" | "lrclib" | "manual" | "none"
  instrumental: boolean
  editable: boolean
}

export interface DiscoverSource {
  key: string
  name: string
  playlists: boolean
  links: boolean
}

export interface DiscoverResult {
  source: string
  id: string
  title: string
  artist: string
  genre?: string
  year?: number
  duration?: number
  coverUrl?: string
  pageUrl?: string
  previewUrl?: string
  license?: string
  trackId?: string
}

export interface DiscoverSearch {
  results: DiscoverResult[]
  page: number
  hasMore: boolean
  playlist?: DiscoverPlaylist
}

export interface DiscoverPlaylist {
  id: string
  title: string
  count: number
  total?: number
}

export interface AppRelease {
  version: string
  fileName: string
  size: number
  modifiedAt: string
}

export interface TrackChanges {
  tracks: Track[]
  deleted: string[]
  now: string
}

export interface TrackShare {
  token: string
  trackId: string
  createdAt: string
  expiresAt?: string
  plays: number
  mine: boolean
}

export interface AdminShare {
  token: string
  trackId: string
  title: string
  artist: string
  sharedBy: string
  createdAt: string
  expiresAt: string
  plays: number
}

export interface SharedTrack {
  title: string
  artist: string
  album: string
  duration: number
  hasCover: boolean
  sharedBy: string
  coverColor?: string
}

export interface DuplicateGroup {
  reason: string
  tracks: Track[]
}

export interface PlaylistImport {
  id: string
  source: string
  listId: string
  title: string
  state: "queued" | "listing" | "running" | "done" | "cancelled" | "failed"
  total: number
  done: number
  added: number
  existing: number
  failed: number
  current?: string
  playlistId?: string
  error?: string
  failures: { title: string; reason: string }[]
  existingTracks: { title: string; trackId: string; reason: string }[]
  possibleDuplicates: { title: string; trackId: string; existingId: string; existingTitle: string; existingArtist: string }[]
  startedAt: string
  finishedAt?: string
}

export interface Waveform {
  peaks: number[]
}

export interface AdminUser extends User {
  trackCount: number
  bytesUsed: number
  lastPlayedAt: string | null
}

export interface AdminSettings {
  registrationOpen: boolean
  registrationPinned: boolean
  inviteRequired: boolean
  transcodingAvailable: boolean
  userQuotaBytes: number
  metadataLookupPaused: boolean
}

export interface SearchResults {
  tracks: Track[]
  tracksTotal?: number
  albums: Album[]
  artists: Artist[]
  playlists: Playlist[]
}

export type TrackPatch = Partial<Pick<Track, "title" | "artist" | "album" | "genre" | "year">>

export interface MetadataCandidate {
  id: string
  source: string
  title: string
  artist: string
  album: string
  genre: string
  year: number
  duration: number
  hasArtwork: boolean
  confidence: number
  applied: boolean
}

export interface PlaybackState {
  track: Track
  position: number
  queue: Track[]
  queueIndex: number
  updatedAt: string
}

export interface MetadataCandidatesResponse {
  autoApplied: boolean
  candidates: MetadataCandidate[]
  lookupDisabled: boolean
  pending: boolean
  queuePosition?: number
  partial: boolean
  attempts: number
  retryAt?: string
  paused: boolean
}

export interface DuplicateMatch {
  track: Track
  reason: string
}

export interface DuplicateResponse {
  error: string
  pendingId: string
  matches: DuplicateMatch[]
  upload: { title: string; artist: string; album: string; duration: number; fileName: string }
}

export interface MetadataQueueStatus {
  queued: number
  running: { trackId: string; title: string } | null
  nextRetryAt?: string
  paused: boolean
  disabled: boolean
}


export interface AuthConfig {
  registrationOpen: boolean
  inviteRequired: boolean
  minPasswordLength: number
}

export interface Invite {
  code: string
  note: string
  createdBy: string
  createdAt: string
  expiresAt: string
  usedBy: string | null
  usedAt: string | null
  revokedAt: string | null
  status: "pending" | "used" | "expired" | "revoked"
}

export interface InviteCheck {
  valid: boolean
  status: "pending" | "used" | "expired" | "revoked" | "invalid"
  expiresAt?: string
  invitedBy?: string
}

export interface TagChange {
  field: "title" | "artist" | "album" | "genre" | "year"
  current: string
  file: string
}

export interface FileTagDiff {
  track: Track
  changes: TagChange[]
}

export type AttentionReason = "artist" | "album" | "cover" | "thumbnail"

export interface AttentionItem {
  trackId: string
  reasons: AttentionReason[]
}

export interface ActivityEvent {
  at: string
  context: string
  message: string
  count: number
}

export interface TrimEvent {
  at: string
  trackId: string
  title: string
  fromStart: number
  fromEnd: number
}

export interface SourceHealth {
  key: string
  name: string
  lastOk?: string
  lastError?: string
  lastErrorAt?: string
  failingSince?: string
}

export interface ActivityStatus {
  silence: { enabled: boolean; rule: string; total: number; pending: number; recentCuts: TrimEvent[] }
  analysis: { enabled: boolean; waiting: number; current: { id: string; title: string } | null }
  imports: PlaylistImport[]
  sources: SourceHealth[]
  ytdlp: { version: string; path: string; jsRuntime: string } | null
  disk: { libraryBytes: number; databaseBytes: number; freeBytes: number; totalBytes: number }
  errors: ActivityEvent[]
}
