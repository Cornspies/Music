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
  albumArtist?: string
  codec?: string
  bitrate?: number
  sampleRate?: number
  channels?: number
  bitDepth?: number
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

export type Role = "user" | "admin" | "demo"

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
  artistCount?: number
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
  coverVersion: string
}

export interface PlaylistAddResult {
  added: number
  skipped: number
}

export interface PasswordReset {
  code: string
  username: string
  expiresAt: string
}

export interface PasswordResetCheck {
  valid: boolean
  username?: string
  expiresAt?: string
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
  next?: string
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
  songs: boolean
}

export interface DiscoverResult {
  source: string
  id: string
  title: string
  artist: string
  album?: string
  genre?: string
  year?: number
  duration?: number
  coverUrl?: string
  pageUrl?: string
  previewUrl?: string
  license?: string
  trackId?: string
}

export interface AudioQuality {
  codec: string
  kbps: number
}

export interface UpgradeCandidate {
  track: Track
  current: AudioQuality
  found: DiscoverResult
  quality: AudioQuality
  source: string
  estimatedBytes: number
}

export interface UpgradeStatus {
  running: boolean
  source: string
  total: number
  done: number
  skipped: number
  startedAt?: string
  finishedAt?: string
  error?: string
  candidates: UpgradeCandidate[]
  sources: Pick<DiscoverSource, "key" | "name">[]
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

export type TrackPatch = Partial<Pick<Track, "title" | "artist" | "album" | "albumArtist" | "genre" | "year">>

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

export interface ListenMember {
  id: string
  username: string
  hasAvatar: boolean
  isHost: boolean
  online: boolean
}

export interface ListenEntry {
  id: string
  track: Track
  addedBy: Uploader
}

export interface ListenState {
  id: string
  code?: string
  host: Uploader
  isHost: boolean
  canControl: boolean
  everyoneCanControl: boolean
  capacity: number
  members: ListenMember[]
  version: number
  queueVersion: number
  entries?: ListenEntry[]
  current: string
  playing: boolean
  position: number
}

export interface ListenInvite {
  id: string
  host: Uploader
  guests: number
  capacity: number
  full: boolean
  joined: boolean
  nowPlaying?: { title: string; artist: string }
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
  upload: {
    title: string
    artist: string
    album: string
    duration: number
    fileName: string
    fileSize: number
    mimeType: string
    storageBytes: number
  }
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
  field: "title" | "artist" | "album" | "albumArtist" | "genre" | "year"
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

export interface BulkApplyStatus {
  running: boolean
  total: number
  done: number
  failed: number
}

export interface BulkApplyResult {
  applying: number
  lookups: number
  uncertain: number
}

export interface HomeMix {
  id: string
  kind: "artist" | "repeat" | "rediscover"
  title: string
  subtitle: string
  seed?: string
  trackIds: string[]
  coverTrackIds: string[]
}

export interface HomeArtist {
  name: string
  trackCount: number
  listens: number
  coverTrackId?: string
  picture?: string
}

export interface HomeSuggestion {
  name: string
  deezerId?: number
  picture?: string
  trackCount: number
  because: string
}

export interface HomeSimilar {
  trackIds: string[]
  basedOn: string[]
}

export interface HomeFeed {
  mixes: HomeMix[]
  topArtists: HomeArtist[]
  suggestedArtists: HomeSuggestion[]
  similar: { week: HomeSimilar; month: HomeSimilar }
  pending: boolean
}

export interface CatalogArtist {
  id: number
  name: string
  picture?: string
  fans: number
  releases: number
  trackCount: number
}

export interface CatalogRelease {
  id: number
  title: string
  artist: string
  type: string
  year: number
  cover?: string
  trackCount: number
}

export interface CatalogTrack {
  id: number
  title: string
  artist: string
  album?: string
  albumId?: number
  cover?: string
  duration: number
  position?: number
  disc?: number
  trackId?: string
  year?: number
  found?: DiscoverResult[]
}

export interface CatalogSongSource {
  key: string
  name: string
  count: number
  error?: string
}

export interface CatalogArtistSongs {
  songs: CatalogTrack[]
  sources: CatalogSongSource[]
}

export interface CatalogArtistPage {
  artist: CatalogArtist
  top: CatalogTrack[]
  releases: CatalogRelease[]
}

export interface CatalogReleasePage {
  release: CatalogRelease
  tracks: CatalogTrack[]
}

export interface LookupSource {
  key: string
  name: string
}

export interface FieldOption {
  source: string
  title: string
  artist: string
  album: string
  albumArtist?: string
  year: number
  genre: string
  type: string
  cover?: string
  duration: number
  url?: string
}

export interface LibraryPin {
  kind: "artist" | "album"
  name: string
  artist: string
  pinnedAt: string
}
