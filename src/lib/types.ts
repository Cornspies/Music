// API types — mirror the Go backend JSON contract exactly.

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
  /** ISO-8601 timestamp of the last play, null when never played */
  lastPlayedAt: string | null
  /** Who contributed this file — the library is shared between all users. */
  uploadedBy: Uploader
  /**
   * True when the server also holds a widely-playable copy, made at upload
   * time for formats some platforms can't decode (Ogg on iOS, chiefly).
   * The original remains the default; request `?version=compatible` for this.
   */
  hasCompatible: boolean
  compatibleMimeType: string
  compatibleFileSize: number
  /** Bumped on every tag/cover/copy change; drives GET /api/tracks/changes. */
  updatedAt: string
  /** Integrated loudness in LUFS (EBU R128), null until analysed. */
  loudness: number | null
  /** True once GET /api/tracks/{id}/waveform has peaks. */
  hasWaveform: boolean
  /** Catalogue a Discover-added track came from ("ncs"); "" for uploads. */
  source: string
  sourceId: string
  /** "#rrggbb" representative colour of the cover, "" until computed. */
  coverColor: string
  /** Online lookup state: "queued" | "running" | "waiting" (retry parked, a provider was missing) | "done" | "" (never queued). */
  metadataStatus: string
  /** The audio was cut and the uploaded original is kept for "restore". */
  trimmed: boolean
}

/** GET /api/tracks/{id}/silence — suggested cut points. */
export interface SilenceInfo {
  duration: number
  /** Where sound first starts (0 when it starts at once). */
  leadingEnd: number
  /** Where sound last stops (== duration when it runs to the end). */
  trailingStart: number
}

/** Small public profile shown next to a track. */
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
  /** "admin" can manage users and settings; the first account is one. */
  role: Role
}

/** Short-lived, read-only token for media URLs (GET /api/auth/media-token). */
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
  /** Public playlists can be opened and played by any signed-in user. */
  isPublic: boolean
  /** Who created it — shown on playlists shared by someone else. */
  owner: Uploader
  /** True when the signed-in user owns it (only owners can edit). */
  isMine: boolean
  /** Someone else's playlist the viewer keeps in their sidebar. */
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
  /** Distinct tracks, most recent listen first (from the listening history). */
  recentlyPlayed: Track[]
  /** Total seconds of everything this user has played, across devices. */
  listenedSeconds: number
  playCount: number
}

/** One listen, from GET /api/history. */
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


/** GET /api/tracks/{id}/waveform */
/** One time-stamped line of synced lyrics. */
export interface LyricLine {
  /** Seconds from the start at which the line begins. */
  t: number
  line: string
}

/** GET /api/tracks/{id}/lyrics. */
export interface Lyrics {
  plain: string
  /** null when only plain text is known. */
  synced: LyricLine[] | null
  /** "embedded" (from the file's tag), "lrclib", "manual", or "none". */
  source: "embedded" | "lrclib" | "manual" | "none"
  instrumental: boolean
  /** The uploader and admins may edit. */
  editable: boolean
}

/** An online catalogue Discover can search ("ncs", "youtube"). */
export interface DiscoverSource {
  key: string
  name: string
  /** A pasted playlist link can be imported as a whole. */
  playlists: boolean
}

/** One Discover search hit. */
export interface DiscoverResult {
  source: string
  id: string
  title: string
  artist: string
  genre?: string
  year?: number
  /** Seconds, when the catalogue lists it. */
  duration?: number
  coverUrl?: string
  /** The track's page on the catalogue's site. */
  pageUrl?: string
  /** Streams the track from the catalogue, for a listen before adding. */
  previewUrl?: string
  /** Set when this result is already in the library. */
  trackId?: string
}

export interface DiscoverSearch {
  results: DiscoverResult[]
  page: number
  hasMore: boolean
  /** Set when the query was a playlist link: the results are its tracks. */
  playlist?: DiscoverPlaylist
}

/** A catalogue playlist behind a pasted link. */
export interface DiscoverPlaylist {
  id: string
  title: string
  /** Tracks listed (and imported); `total` is the playlist's full size when a long one was cut short. */
  count: number
  total?: number
}

/** One Android build the server offers (GET /api/app/releases). */
export interface AppRelease {
  version: string
  fileName: string
  size: number
  /** When the file was put on the server. */
  modifiedAt: string
}

/** Tracks changed since a timestamp (GET /api/tracks/changes). */
export interface TrackChanges {
  tracks: Track[]
  deleted: string[]
  /** Pass this back as `since` on the next call. */
  now: string
}

/** A share link to one track: anyone who has it can play that track. */
export interface TrackShare {
  token: string
  trackId: string
  createdAt: string
  /** "" (absent) for a link that doesn't expire. */
  expiresAt?: string
  /** How often the link has been opened. */
  plays: number
  /** True when the signed-in user created this link. */
  mine: boolean
}

/** What the public share page knows about the track behind a link. */
export interface SharedTrack {
  title: string
  artist: string
  album: string
  duration: number
  hasCover: boolean
  /** Who made the link. */
  sharedBy: string
  coverColor?: string
}

/** Library tracks that look like one song (GET /api/library/duplicates). */
export interface DuplicateGroup {
  /** "same title and artist" or "same title and length". */
  reason: string
  /** Oldest first. */
  tracks: Track[]
}

/** A playlist import running on the server. */
export interface PlaylistImport {
  id: string
  source: string
  /** The catalogue's id for the playlist. */
  listId: string
  title: string
  state: "queued" | "listing" | "running" | "done" | "cancelled" | "failed"
  total: number
  done: number
  added: number
  existing: number
  failed: number
  /** The title being fetched right now. */
  current?: string
  /** The library playlist the tracks go into, once it exists. */
  playlistId?: string
  /** Why the whole import stopped. */
  error?: string
  failures: { title: string; reason: string }[]
  /** Entries whose video was in the library already; they were only linked. */
  existingTracks: { title: string; trackId: string; reason: string }[]
  /** Entries added although a library track looks like the same song — worth a look. */
  possibleDuplicates: { title: string; trackId: string; existingId: string; existingTitle: string; existingArtist: string }[]
  startedAt: string
  finishedAt?: string
}

export interface Waveform {
  /** 200 peaks, each 0-100, left to right. */
  peaks: number[]
}

/** A user as the admin screen sees them (GET /api/admin/users). */
export interface AdminUser extends User {
  trackCount: number
  bytesUsed: number
  lastPlayedAt: string | null
}

/** Runtime settings an admin can change (GET/PATCH /api/admin/settings). */
export interface AdminSettings {
  registrationOpen: boolean
  /** True when MUSIC_ALLOW_REGISTRATION pins it and the toggle is read-only. */
  registrationPinned: boolean
  inviteRequired: boolean
  transcodingAvailable: boolean
  userQuotaBytes: number
}

export interface SearchResults {
  tracks: Track[]
  albums: Album[]
  artists: Artist[]
  playlists: Playlist[]
}

export type TrackPatch = Partial<Pick<Track, "title" | "artist" | "album" | "genre" | "year">>

/** A single metadata match found online, offered to the user to confirm. */
export interface MetadataCandidate {
  id: string
  /** "MusicBrainz" | "Deezer" | "iTunes" | "YouTube" */
  source: string
  title: string
  artist: string
  album: string
  genre: string
  year: number
  duration: number
  hasArtwork: boolean
  /** 0-1 match score; the list arrives sorted by this, best first. */
  confidence: number
  /** True for the candidate auto-applied during upload, if any. */
  applied: boolean
}

/** Where the user left off, as remembered by the server (GET /api/playback). */
export interface PlaybackState {
  track: Track
  /** Seconds into the track. */
  position: number
  /** The "up next" list, current track included; deleted tracks already dropped. */
  queue: Track[]
  queueIndex: number
  updatedAt: string
}

export interface MetadataCandidatesResponse {
  autoApplied: boolean
  candidates: MetadataCandidate[]
  lookupDisabled: boolean
  /** True while the lookup is queued or running; candidates are partial. Poll. */
  pending: boolean
  /** 1 = next up (or running now). */
  queuePosition?: number
  /** A finished lookup that was missing a provider. */
  partial: boolean
  attempts: number
  /** When a parked retry runs (RFC 3339), while a provider is missing. */
  retryAt?: string
}

/** One existing track that looks like an upload (409 from POST /api/tracks). */
export interface DuplicateMatch {
  track: Track
  /** "same file" | "same song" */
  reason: string
}

export interface DuplicateResponse {
  error: string
  /** Confirms (POST) or discards (DELETE) the held file: /api/uploads/pending/{id}. */
  pendingId: string
  matches: DuplicateMatch[]
  upload: { title: string; artist: string; album: string; duration: number; fileName: string }
}

/** GET /api/metadata/queue */
export interface MetadataQueueStatus {
  queued: number
  running: { trackId: string; title: string } | null
  /** Set when everything left is waiting on a provider's rate limit. */
  nextRetryAt?: string
  disabled: boolean
}


/**
 * Sign-up policy, fetched before the auth form renders so it can show the
 * right fields. GET /api/auth/config (unauthenticated).
 */
export interface AuthConfig {
  registrationOpen: boolean
  inviteRequired: boolean
  minPasswordLength: number
}

/** A single-use invite link (admin console). */
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

/** GET /api/auth/invite/{code} — whether a link still works. */
export interface InviteCheck {
  valid: boolean
  status: "pending" | "used" | "expired" | "revoked" | "invalid"
  expiresAt?: string
  invitedBy?: string
}
