package com.music.app.api

import com.music.app.unplayableMimeTypes

import kotlinx.serialization.Serializable

/**
 * Data classes mirroring the Go server's JSON responses exactly
 * (see /home/z/my-project/worklog.md — "API Contract").
 * All field names are camelCase and match the server tags one-to-one.
 */

@Serializable
data class User(
    val id: String,
    val username: String,
    val createdAt: String = "",
    val hasAvatar: Boolean = false,
    /** "user" or "admin". Admins can manage users and edit anyone's track. */
    val role: String = "user",
) {
    val isAdmin: Boolean get() = role == "admin"
}

/** Short-lived, read-only token for media URLs (GET /api/auth/media-token). */
@Serializable
data class MediaToken(
    val token: String,
    val expiresAt: String = "",
)

/** Small public profile shown next to a track in the shared library. */
@Serializable
data class Uploader(
    val id: String = "",
    val username: String = "",
    val hasAvatar: Boolean = false,
)

@Serializable
data class AuthResponse(
    val token: String,
    val user: User,
)

@Serializable
data class Track(
    val id: String,
    val title: String,
    val artist: String = "",
    val album: String = "",
    val genre: String = "",
    val year: Int = 0,
    val trackNo: Int = 0,
    val duration: Double = 0.0,
    val mimeType: String = "",
    val fileSize: Long = 0L,
    val hasCover: Boolean = false,
    val plays: Int = 0,
    val isFavorite: Boolean = false,
    val createdAt: String = "",
    /**
     * Who contributed this file — the library is shared between all users.
     * Defaulted so tracks cached offline before this field existed still decode.
     */
    val uploadedBy: Uploader = Uploader(),
    /**
     * True when the server also holds a widely-playable copy, made at upload
     * time for formats this platform may not decode (Ogg on iOS). Stream it
     * with ?version=compatible.
     */
    val hasCompatible: Boolean = false,
    val compatibleMimeType: String = "",
    val compatibleFileSize: Long = 0,
    /** Bumped on every tag/cover/copy change; drives /api/tracks/changes. */
    val updatedAt: String = "",
    /** Integrated loudness in LUFS (EBU R128); null until analysed. */
    val loudness: Double? = null,
    /** True once /api/tracks/{id}/waveform has peaks. */
    val hasWaveform: Boolean = false,
    /** Catalogue a Discover-added track came from ("ncs"); "" for uploads. */
    val source: String = "",
    val sourceId: String = "",
    /** "#rrggbb" representative colour of the cover, "" until computed. */
    val coverColor: String = "",
    /** Online lookup state: "queued" | "running" | "done" | "" (never queued). */
    val metadataStatus: String = "",
)

/** An online catalogue Discover can search. */
@Serializable
data class DiscoverSource(
    val key: String,
    val name: String,
    /** A pasted playlist link can be imported as a whole. */
    val playlists: Boolean = false,
)

/** One Discover search hit. */
@Serializable
data class DiscoverResult(
    val source: String,
    val id: String,
    val title: String = "",
    val artist: String = "",
    val genre: String? = null,
    val year: Int? = null,
    /** Seconds, when the catalogue lists it. */
    val duration: Double? = null,
    val coverUrl: String? = null,
    val pageUrl: String? = null,
    val previewUrl: String? = null,
    /** Set when this result is already in the library. */
    val trackId: String? = null,
)

@Serializable
data class DiscoverSearch(
    val results: List<DiscoverResult> = emptyList(),
    val page: Int = 1,
    val hasMore: Boolean = false,
    /** Set when the query was a playlist link: the results are its tracks. */
    val playlist: DiscoverPlaylist? = null,
)

/** A catalogue playlist behind a pasted link. [total] is its full size when a long one was cut short. */
@Serializable
data class DiscoverPlaylist(
    val id: String,
    val title: String = "",
    val count: Int = 0,
    val total: Int? = null,
)

@Serializable
data class DiscoverAddRequest(val source: String, val id: String)

@Serializable
data class ImportRequest(val source: String, val url: String)

/** A playlist import running on the server. */
@Serializable
data class PlaylistImport(
    val id: String,
    val source: String = "",
    /** The catalogue's id for the playlist. */
    val listId: String = "",
    val title: String = "",
    /** queued, listing, running, done, cancelled or failed. */
    val state: String = "",
    val total: Int = 0,
    val done: Int = 0,
    val added: Int = 0,
    val existing: Int = 0,
    val failed: Int = 0,
    /** The title being fetched right now. */
    val current: String? = null,
    /** The library playlist the tracks go into, once it exists. */
    val playlistId: String? = null,
    /** Why the whole import stopped. */
    val error: String? = null,
    val failures: List<ImportFailure> = emptyList(),
    val startedAt: String = "",
    val finishedAt: String? = null,
) {
    val active: Boolean get() = state == "queued" || state == "listing" || state == "running"
}

@Serializable
data class ImportFailure(val title: String = "", val reason: String = "")

@Serializable
data class ListenedRequest(val seconds: Double)

/** One existing track that looks like an upload (409 from POST /api/tracks). */
@Serializable
data class DuplicateMatch(val track: Track, val reason: String = "")

@Serializable
data class DuplicateResponse(
    val error: String = "",
    /** Confirms (POST) or discards (DELETE) the held file: /api/uploads/pending/{id}. */
    val pendingId: String = "",
    val matches: List<DuplicateMatch> = emptyList(),
)

@Serializable
data class Playlist(
    val id: String,
    val name: String,
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
    val createdAt: String = "",
    /** Public playlists can be opened and played by any signed-in user. */
    val isPublic: Boolean = false,
    /** Who created it — shown on playlists shared by someone else. */
    val owner: Uploader = Uploader(),
    /** True when the signed-in user owns it; only owners can edit. */
    val isMine: Boolean = true,
    /** Someone else's playlist the viewer keeps in their sidebar/list. */
    val isPinned: Boolean = false,
)

/** GET /api/playlists/{id} — playlist summary plus its ordered tracks. */
@Serializable
data class PlaylistDetail(
    val id: String,
    val name: String,
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
    val createdAt: String = "",
    val isPublic: Boolean = false,
    val owner: Uploader = Uploader(),
    val isMine: Boolean = true,
    val isPinned: Boolean = false,
    val tracks: List<Track> = emptyList(),
)

/**
 * The list-shaped view of a playlist detail.
 *
 * [PlaylistDetail] is a separate payload rather than a subtype of [Playlist]
 * (the detail endpoint returns the tracks as well), so anything that stores or
 * lists playlists needs this conversion.
 */
fun PlaylistDetail.toPlaylist(): Playlist = Playlist(
    id = id,
    name = name,
    trackCount = trackCount,
    totalDuration = totalDuration,
    createdAt = createdAt,
    isPublic = isPublic,
    owner = owner,
    isMine = isMine,
    isPinned = isPinned,
)

@Serializable
data class Album(
    val name: String,
    val artist: String = "",
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
    val hasCover: Boolean = false,
    val coverTrackId: String = "",
)

@Serializable
data class Artist(
    val name: String,
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
)

@Serializable
data class SearchResults(
    val tracks: List<Track> = emptyList(),
    val albums: List<Album> = emptyList(),
    val artists: List<Artist> = emptyList(),
    val playlists: List<Playlist> = emptyList(),
)

@Serializable
data class Stats(
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
    val artistCount: Int = 0,
    val albumCount: Int = 0,
    val favoriteCount: Int = 0,
    val playlistCount: Int = 0,
    val mostPlayed: List<Track> = emptyList(),
    /** Distinct tracks, most recent listen first, from the server's history. */
    val recentlyPlayed: List<Track> = emptyList(),
    /** Seconds of everything this user has played, on every device. */
    val listenedSeconds: Double = 0.0,
    val playCount: Int = 0,
)


/** One listen, from GET /api/history. */
@Serializable
data class PlayEvent(
    val track: Track,
    val playedAt: String = "",
)

@Serializable
data class HistoryResponse(
    val events: List<PlayEvent> = emptyList(),
    val listenedSeconds: Double = 0.0,
    val playCount: Int = 0,
    val trackCount: Int = 0,
)

/** GET /api/tracks/changes?since= — what changed, for cheap re-syncs. */
@Serializable
data class TrackChanges(
    val tracks: List<Track> = emptyList(),
    val deleted: List<String> = emptyList(),
    /** Server time to hand back as the next `since`. */
    val now: String = "",
)

/** One time-stamped line of synced lyrics. */
@Serializable
data class LyricLine(
    /** Seconds from the start at which the line begins. */
    val t: Double = 0.0,
    val line: String = "",
)

/** GET /api/tracks/{id}/lyrics; source "none" means nothing was found. */
@Serializable
data class Lyrics(
    val plain: String = "",
    /** Null when only plain text is known. */
    val synced: List<LyricLine>? = null,
    /** "embedded", "lrclib", "manual" or "none". */
    val source: String = "none",
    val instrumental: Boolean = false,
    val editable: Boolean = false,
) {
    val found: Boolean get() = source != "none"
}

/** GET /api/tracks/{id}/waveform */
@Serializable
data class Waveform(
    /** 200 peaks, 0-100, left to right. */
    val peaks: List<Int> = emptyList(),
)

/**
 * One online metadata match for a track (MusicBrainz / Deezer / iTunes / YouTube),
 * offered to the user to confirm after an upload.
 * GET /api/tracks/{id}/metadata/candidates
 */
@Serializable
data class MetadataCandidate(
    val id: String,
    /** "MusicBrainz" | "Deezer" | "iTunes" | "YouTube". */
    val source: String = "",
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    val genre: String = "",
    val year: Int = 0,
    val duration: Double = 0.0,
    val hasArtwork: Boolean = false,
    /** 0-1 match score; the list arrives sorted by this, best first. */
    val confidence: Double = 0.0,
    /** True for the candidate the server auto-applied during upload, if any. */
    val applied: Boolean = false,
)

@Serializable
data class MetadataCandidatesResponse(
    val autoApplied: Boolean = false,
    val candidates: List<MetadataCandidate> = emptyList(),
    /** True when online lookup is switched off server-side. */
    val lookupDisabled: Boolean = false,
    /** True while the lookup is queued or running; [candidates] is what's been found so far. */
    val pending: Boolean = false,
    /** 1 = next up (or running now). */
    val queuePosition: Int = 0,
    /** A finished lookup that was missing a provider. */
    val partial: Boolean = false,
    val attempts: Int = 0,
)

/** GET /api/metadata/queue */
@Serializable
data class MetadataQueueStatus(
    val queued: Int = 0,
    val running: RunningLookup? = null,
    val nextRetryAt: String? = null,
    val disabled: Boolean = false,
) {
    @Serializable
    data class RunningLookup(val trackId: String = "", val title: String = "")
}

/**
 * Where the user left off, as remembered by the server (GET /api/playback).
 * The queue comes back as full tracks, with anything deleted since already
 * dropped, so it can be restored without further requests.
 */
@Serializable
data class PlaybackState(
    val track: Track,
    /** Seconds into the track. */
    val position: Double = 0.0,
    val queue: List<Track> = emptyList(),
    val queueIndex: Int = 0,
    val updatedAt: String = "",
)

// ---- request bodies ----

/** What a playback save answers with. */
@Serializable
data class PlaybackSaved(val updatedAt: String = "")

@Serializable
data class PlaybackRequest(
    val trackId: String,
    val position: Double,
    /** Track IDs of the "up next" list. Null means "unchanged" — sent only when it changed. */
    val queue: List<String>? = null,
)

@Serializable
data class AuthRequest(
    val username: String,
    val password: String,
    /** Only sent when the server reports that an invite code is required. */
    val inviteCode: String? = null,
)

/**
 * Sign-up policy, fetched before the auth form renders so it shows the right
 * fields. GET /api/auth/config (unauthenticated).
 */
@Serializable
data class AuthConfig(
    val registrationOpen: Boolean = true,
    val inviteRequired: Boolean = false,
    val minPasswordLength: Int = 10,
)

/** POST /api/auth/password */
@Serializable
data class ChangePasswordRequest(
    val currentPassword: String,
    val newPassword: String,
)

/** A bare replacement token, as returned after a password change. */
@Serializable
data class TokenResponse(
    val token: String = "",
)

@Serializable
data class PlaylistRequest(
    val name: String,
)

/** PATCH /api/playlists/{id} — both fields optional. */
@Serializable
data class UpdatePlaylistRequest(
    val name: String? = null,
    val isPublic: Boolean? = null,
)

@Serializable
data class AddTrackRequest(
    val trackId: String,
)

/** POST /api/tracks/{id}/metadata/apply */
@Serializable
data class ApplyMetadataRequest(
    val candidateId: String,
)

/** The server's error shape: `{"error": "message"}`. */
@Serializable
data class ApiError(
    val error: String = "",
)


/**
 * Whether this platform should use the server's compatible copy for a track
 * rather than the original upload.
 *
 * True only when a copy exists *and* the original is in a format this platform
 * cannot decode — the original is always preferred otherwise, since the copy is
 * a lossy re-encode that exists only so iOS isn't left silent.
 */
fun prefersCompatible(track: Track): Boolean =
    track.hasCompatible &&
        track.mimeType.substringBefore(';').trim().lowercase() in unplayableMimeTypes
