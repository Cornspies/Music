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
}

/** Small public profile shown next to a track. */
export interface Uploader {
  id: string
  username: string
  hasAvatar: boolean
}

export interface User {
  id: string
  username: string
  createdAt: string
  hasAvatar: boolean
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
