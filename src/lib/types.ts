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

/** GET /api/tracks/changes?since= — delta sync. */
export interface TrackChanges {
  tracks: Track[]
  deleted: string[]
  now: string
}

/** GET /api/tracks/{id}/waveform */
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
}

export interface ChangePasswordPayload {
  currentPassword: string
  newPassword: string
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
