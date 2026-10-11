package com.music.app.api

import com.music.app.unplayableMimeTypes

import kotlinx.serialization.Serializable

@Serializable
data class User(
    val id: String,
    val username: String,
    val createdAt: String = "",
    val hasAvatar: Boolean = false,
    val role: String = "user",
) {
    val isAdmin: Boolean get() = role == "admin"
}

@Serializable
data class MediaToken(
    val token: String,
    val expiresAt: String = "",
)

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
    val uploadedBy: Uploader = Uploader(),
    val hasCompatible: Boolean = false,
    val compatibleMimeType: String = "",
    val compatibleFileSize: Long = 0,
    val updatedAt: String = "",
    val loudness: Double? = null,
    val hasWaveform: Boolean = false,
    val source: String = "",
    val sourceId: String = "",
    val coverColor: String = "",
    val metadataStatus: String = "",
)

@Serializable
data class DiscoverSource(
    val key: String,
    val name: String,
    val playlists: Boolean = false,
    val links: Boolean = false,
)

@Serializable
data class DiscoverResult(
    val source: String,
    val id: String,
    val title: String = "",
    val artist: String = "",
    val genre: String? = null,
    val year: Int? = null,
    val duration: Double? = null,
    val coverUrl: String? = null,
    val pageUrl: String? = null,
    val previewUrl: String? = null,
    val license: String? = null,
    val trackId: String? = null,
)

@Serializable
data class DiscoverSearch(
    val results: List<DiscoverResult> = emptyList(),
    val page: Int = 1,
    val hasMore: Boolean = false,
    val playlist: DiscoverPlaylist? = null,
)

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

@Serializable
data class PlaylistImport(
    val id: String,
    val source: String = "",
    val listId: String = "",
    val title: String = "",
    val state: String = "",
    val total: Int = 0,
    val done: Int = 0,
    val added: Int = 0,
    val existing: Int = 0,
    val failed: Int = 0,
    val current: String? = null,
    val playlistId: String? = null,
    val error: String? = null,
    val failures: List<ImportFailure> = emptyList(),
    val existingTracks: List<ImportExisting> = emptyList(),
    val possibleDuplicates: List<ImportDuplicate> = emptyList(),
    val startedAt: String = "",
    val finishedAt: String? = null,
) {
    val active: Boolean get() = state == "queued" || state == "listing" || state == "running"
}

@Serializable
data class ImportFailure(val title: String = "", val reason: String = "")

@Serializable
data class ImportExisting(val title: String = "", val trackId: String = "", val reason: String = "")

@Serializable
data class ImportDuplicate(
    val title: String = "",
    val trackId: String = "",
    val existingId: String = "",
    val existingTitle: String = "",
    val existingArtist: String = "",
)

@Serializable
data class TrackShare(
    val token: String,
    val trackId: String = "",
    val createdAt: String = "",
    val expiresAt: String? = null,
    val plays: Int = 0,
    val mine: Boolean = false,
)

@Serializable
data class ShareRequest(val expiresInDays: Int = 0)

@Serializable
data class DuplicateGroup(
    val reason: String = "",
    val tracks: List<Track> = emptyList(),
)

@Serializable
data class ListenedRequest(val seconds: Double)

@Serializable
data class DuplicateMatch(val track: Track, val reason: String = "")

@Serializable
data class DuplicateUpload(
    val duration: Double = 0.0,
    val fileSize: Long = 0L,
    val mimeType: String = "",
    val storageBytes: Long = 0L,
)

@Serializable
data class DuplicateResponse(
    val error: String = "",
    val pendingId: String = "",
    val matches: List<DuplicateMatch> = emptyList(),
    val upload: DuplicateUpload = DuplicateUpload(),
)

@Serializable
data class ReplaceTrackRequest(val trackId: String)

@Serializable
data class Playlist(
    val id: String,
    val name: String,
    val trackCount: Int = 0,
    val totalDuration: Double = 0.0,
    val createdAt: String = "",
    val isPublic: Boolean = false,
    val owner: Uploader = Uploader(),
    val isMine: Boolean = true,
    val isPinned: Boolean = false,
)

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
    val tracksTotal: Int? = null,
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
    val recentlyPlayed: List<Track> = emptyList(),
    val listenedSeconds: Double = 0.0,
    val playCount: Int = 0,
)


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

@Serializable
data class TrackChanges(
    val tracks: List<Track> = emptyList(),
    val deleted: List<String> = emptyList(),
    val now: String = "",
)

@Serializable
data class LyricLine(
    val t: Double = 0.0,
    val line: String = "",
)

@Serializable
data class Lyrics(
    val plain: String = "",
    val synced: List<LyricLine>? = null,
    val source: String = "none",
    val instrumental: Boolean = false,
    val editable: Boolean = false,
) {
    val found: Boolean get() = source != "none"
}

@Serializable
data class Waveform(
    val peaks: List<Int> = emptyList(),
)

@Serializable
data class MetadataCandidate(
    val id: String,
    val source: String = "",
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    val genre: String = "",
    val year: Int = 0,
    val duration: Double = 0.0,
    val hasArtwork: Boolean = false,
    val confidence: Double = 0.0,
    val applied: Boolean = false,
)

@Serializable
data class MetadataCandidatesResponse(
    val autoApplied: Boolean = false,
    val candidates: List<MetadataCandidate> = emptyList(),
    val lookupDisabled: Boolean = false,
    val pending: Boolean = false,
    val queuePosition: Int = 0,
    val partial: Boolean = false,
    val attempts: Int = 0,
    val retryAt: String? = null,
    val paused: Boolean = false,
)

@Serializable
data class MetadataQueueStatus(
    val queued: Int = 0,
    val running: RunningLookup? = null,
    val nextRetryAt: String? = null,
    val paused: Boolean = false,
    val disabled: Boolean = false,
) {
    @Serializable
    data class RunningLookup(val trackId: String = "", val title: String = "")
}

@Serializable
data class PlaybackState(
    val track: Track,
    val position: Double = 0.0,
    val queue: List<Track> = emptyList(),
    val queueIndex: Int = 0,
    val updatedAt: String = "",
)

@Serializable
data class PlaybackSaved(val updatedAt: String = "")

@Serializable
data class PlaybackPref(
    val context: String,
    val shuffle: String = "off",
    val repeat: String = "off",
)

@Serializable
data class PlaybackRequest(
    val trackId: String,
    val position: Double,
    val queue: List<String>? = null,
)

@Serializable
data class AuthRequest(
    val username: String,
    val password: String,
    val inviteCode: String? = null,
)

@Serializable
data class AuthConfig(
    val registrationOpen: Boolean = true,
    val inviteRequired: Boolean = false,
    val minPasswordLength: Int = 10,
)

@Serializable
data class ChangePasswordRequest(
    val currentPassword: String,
    val newPassword: String,
)

@Serializable
data class RenameAccountRequest(val username: String, val password: String)

@Serializable
data class DeleteAccountRequest(val password: String)

@Serializable
data class TokenResponse(
    val token: String = "",
)

@Serializable
data class PlaylistRequest(
    val name: String,
)

@Serializable
data class UpdatePlaylistRequest(
    val name: String? = null,
    val isPublic: Boolean? = null,
)

@Serializable
data class AddTrackRequest(
    val trackId: String,
)

@Serializable
data class ApplyMetadataRequest(
    val candidateId: String,
)

@Serializable
data class ApiError(
    val error: String = "",
    val reason: String? = null,
)

@Serializable
data class ListenMember(
    val id: String,
    val username: String = "",
    val hasAvatar: Boolean = false,
    val isHost: Boolean = false,
    val online: Boolean = false,
)

@Serializable
data class ListenEntry(
    val id: String,
    val track: Track,
    val addedBy: Uploader = Uploader(),
)

@Serializable
data class ListenState(
    val id: String,
    val code: String? = null,
    val host: Uploader = Uploader(),
    val isHost: Boolean = false,
    val canControl: Boolean = false,
    val everyoneCanControl: Boolean = false,
    val capacity: Int = 0,
    val members: List<ListenMember> = emptyList(),
    val version: Long = 0,
    val queueVersion: Long = 0,
    val entries: List<ListenEntry>? = null,
    val current: String = "",
    val playing: Boolean = false,
    val position: Double = 0.0,
)

@Serializable
data class ListenInviteTrack(val title: String = "", val artist: String = "")

@Serializable
data class ListenInvite(
    val id: String,
    val host: Uploader = Uploader(),
    val guests: Int = 0,
    val capacity: Int = 0,
    val full: Boolean = false,
    val joined: Boolean = false,
    val nowPlaying: ListenInviteTrack? = null,
)

@Serializable
data class ListenCreateRequest(
    val capacity: Int,
    val everyoneCanControl: Boolean,
    val trackId: String? = null,
    val position: Double = 0.0,
    val upcoming: List<String> = emptyList(),
)

@Serializable
data class ListenControlRequest(
    val action: String,
    val position: Double? = null,
    val entryId: String? = null,
)

@Serializable
data class ListenAddRequest(val trackIds: List<String>, val mode: String)

@Serializable
data class ListenSettingsRequest(
    val capacity: Int? = null,
    val everyoneCanControl: Boolean? = null,
)


fun prefersCompatible(track: Track): Boolean =
    track.hasCompatible &&
        track.mimeType.substringBefore(';').trim().lowercase() in unplayableMimeTypes

@Serializable
data class AppRelease(
    val version: String = "",
    val fileName: String = "",
    val size: Long = 0,
    val modifiedAt: String = "",
)

@Serializable
data class AppUpdateCheck(
    val latest: AppRelease? = null,
    val updateAvailable: Boolean = false,
)
