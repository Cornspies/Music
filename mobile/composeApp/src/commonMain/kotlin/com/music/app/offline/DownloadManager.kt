package com.music.app.offline

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.music.app.api.ApiClient
import com.music.app.api.Playlist
import com.music.app.api.PlaylistDetail
import com.music.app.api.Track
import com.music.app.api.User
import com.music.app.api.friendlyError
import com.music.app.api.prefersCompatible
import com.music.app.api.toPlaylist
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json

/**
 * What to keep when a track is stored in two versions.
 *
 * Only a minority of tracks have two — an Ogg/WebM/WMA upload and the AAC copy
 * the server made so iOS can play it. Everything else has exactly one version
 * and this setting does nothing for it.
 *
 * Deliberately two options rather than a list of formats or bitrates: the
 * server holds the original and one AAC copy, nothing else, so anything more
 * elaborate would be offering choices that don't exist.
 */
enum class DownloadQuality {
    /** Keep the file as uploaded. The copy is a lossy re-encode, so this is
     *  always the better-sounding one. */
    ORIGINAL,

    /** Keep whichever version is actually smaller on disk, compared by real
     *  byte size rather than assumed by format — a low-bitrate Opus original
     *  can easily be smaller than its 256k AAC copy. */
    SMALLER,
}

/** Per-track download status, as shown on the download button. */
enum class DownloadState { NONE, DOWNLOADING, DONE, FAILED }

/**
 * Downloads tracks for offline playback and keeps the index of what's on device.
 *
 * The index (a JSON list of [Track]) is persisted alongside the audio, so the
 * Downloads screen and offline playback work with no server reachable at all.
 * It's reconciled against the filesystem on startup, so a file deleted by the
 * OS (or a failed write) can't leave a phantom entry.
 *
 * All state here is Compose snapshot state — screens observing it recompose
 * automatically.
 */
class DownloadManager(
    private val api: ApiClient,
    private val store: OfflineStore,
) {
    private val json = Json { ignoreUnknownKeys = true }

    /** trackId → Track, for everything currently downloaded. */
    private val index = mutableStateMapOf<String, Track>()

    /** Transient per-track status (only for in-flight/failed; DONE is derived from [index]). */
    private val transient = mutableStateMapOf<String, DownloadState>()

    /** Total bytes on device, refreshed whenever downloads change. */
    var bytesUsed by mutableStateOf(0L)
        private set

    /** Message from the most recent failure, for the UI to surface. */
    var lastError by mutableStateOf<String?>(null)
        private set

    /** Which version to keep when a track has two. Persisted across launches. */
    var downloadQuality by mutableStateOf(DownloadQuality.ORIGINAL)
        private set

    fun updateDownloadQuality(quality: DownloadQuality) {
        downloadQuality = quality
        store.saveMeta(KEY_QUALITY, quality.name)
    }

    /** Hold downloads back on mobile data. Persisted across launches. */
    var wifiOnly by mutableStateOf(false)
        private set

    fun updateWifiOnly(on: Boolean) {
        wifiOnly = on
        store.saveMeta(KEY_WIFI_ONLY, if (on) "1" else "0")
    }

    /** True when the Wi-Fi-only setting is stopping downloads right now. */
    val waitingForWifi: Boolean
        get() = wifiOnly && NetworkMonitor.isMetered()

    /** True while [syncAll] runs. */
    var syncing by mutableStateOf(false)
        private set

    /** When the last successful sync finished, for the Downloads screen. */
    var lastSyncedAt by mutableStateOf<String?>(null)
        private set

    /**
     * Whether to store the compatible copy for [track].
     *
     * A platform that cannot decode the original overrides the setting — a
     * smaller file that won't play is worth nothing.
     */
    fun useCompatibleFor(track: Track): Boolean {
        if (prefersCompatible(track)) return true
        if (!track.hasCompatible) return false
        return when (downloadQuality) {
            DownloadQuality.ORIGINAL -> false
            DownloadQuality.SMALLER ->
                track.compatibleFileSize > 0 && track.compatibleFileSize < track.fileSize
        }
    }

    /** Playlist ids the user has explicitly downloaded, with their track lists. */
    private val playlistIndex = mutableStateMapOf<String, Playlist>()

    /** Track ids belonging to each downloaded playlist, so removal is precise. */
    private var playlistTracks: Map<String, List<String>> = emptyMap()

    // Everything the init block touches is declared above it on purpose:
    // Kotlin runs property initialisers and init blocks in declaration order,
    // so a property declared below would still be null here.
    /**
     * Playlists the user asked to *keep* offline: re-synced on every launch so
     * tracks added on another device arrive and removed ones go. A plain
     * one-off playlist download stays exactly as downloaded.
     */
    private val keptPlaylists = mutableStateMapOf<String, Boolean>()

    init {
        reconcile()
        reconcilePlaylists()
        downloadQuality = store.loadMeta(KEY_QUALITY)
            ?.let { saved -> DownloadQuality.entries.firstOrNull { it.name == saved } }
            ?: DownloadQuality.ORIGINAL
        wifiOnly = store.loadMeta(KEY_WIFI_ONLY) == "1"
        lastSyncedAt = store.loadMeta(KEY_SYNC_AT)
        store.loadMeta(KEY_KEPT)?.split(',')?.filter { it.isNotBlank() }?.forEach { keptPlaylists[it] = true }
    }

    /** Downloaded tracks, alphabetical. */
    val tracks: List<Track>
        get() = index.values.sortedBy { it.title.lowercase() }

    val count: Int get() = index.size

    fun stateOf(trackId: String): DownloadState = when {
        index.containsKey(trackId) -> DownloadState.DONE
        else -> transient[trackId] ?: DownloadState.NONE
    }

    /** Local filesystem path for offline playback, or null when not downloaded. */
    fun localPathFor(trackId: String): String? =
        if (index.containsKey(trackId)) store.pathFor(trackId) else null

    /** Downloads [track], replacing any previous copy. Safe to call twice. */
    /**
     * Downloads a track for offline use.
     *
     * [useCompatible] picks which stored version to keep: null means "whatever
     * this platform can play", which is what playlist downloads and the plain
     * download button use. The track row offers an explicit choice when both
     * versions exist.
     */
    suspend fun download(track: Track, useCompatible: Boolean? = null) {
        if (index.containsKey(track.id)) return
        if (transient[track.id] == DownloadState.DOWNLOADING) return
        if (waitingForWifi) {
            transient[track.id] = DownloadState.FAILED
            lastError = "Waiting for Wi-Fi."
            return
        }

        transient[track.id] = DownloadState.DOWNLOADING
        lastError = null
        try {
            val compatible = useCompatible ?: useCompatibleFor(track)
            val bytes = api.downloadTrackBytes(track, compatible)
            if (bytes.isEmpty()) throw IllegalStateException("The server returned an empty file.")
            store.save(track.id, extensionFor(track, compatible), bytes)
            index[track.id] = track
            transient.remove(track.id)
            persist()
        } catch (e: Exception) {
            transient[track.id] = DownloadState.FAILED
            lastError = friendlyError(e)
            // Don't leave a partial file behind for a track we're not tracking.
            store.delete(track.id)
        }
    }

    fun remove(trackId: String) {
        store.delete(trackId)
        index.remove(trackId)
        transient.remove(trackId)
        persist()
    }

    fun removeAll() {
        store.deleteAll()
        index.clear()
        transient.clear()
        persist()
    }

    /** Clears the failed marker so the button returns to "download". */
    fun clearFailure(trackId: String) {
        if (transient[trackId] == DownloadState.FAILED) transient.remove(trackId)
        lastError = null
    }

    /**
     * Keeps a downloaded track's metadata current after the user edits tags
     * (e.g. applying a metadata match), so the Downloads list doesn't go stale.
     */
    fun refreshMetadata(track: Track) {
        if (index.containsKey(track.id)) {
            index[track.id] = track
            persist()
        }
    }

    // ---------------------------------------------------------------- sync

    fun isKeptOffline(playlistId: String): Boolean = keptPlaylists[playlistId] == true

    /**
     * Turns "keep offline" on or off for a playlist. Turning it on downloads
     * the playlist now (with [tracks]) and re-syncs it on every launch; turning
     * it off leaves whatever is downloaded in place — it just stops following
     * the server.
     */
    suspend fun setKeptOffline(
        playlist: Playlist,
        tracks: List<Track>,
        keep: Boolean,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        if (keep) {
            keptPlaylists[playlist.id] = true
            persistKept()
            downloadPlaylist(playlist, tracks, progress)
        } else {
            keptPlaylists.remove(playlist.id)
            persistKept()
        }
    }

    /**
     * Brings everything on the device up to date with the server:
     *
     *  1. every kept playlist is re-fetched; new tracks are downloaded and
     *     tracks that left the playlist are dropped (unless something else
     *     still needs them);
     *  2. `GET /api/tracks/changes?since=` refreshes the metadata of
     *     downloaded tracks (retagged on the web app, say) and removes any
     *     that were deleted on the server.
     *
     * Cheap when nothing changed — one small request — so it runs on every
     * launch. Honours the Wi-Fi-only setting: on mobile data the metadata
     * still syncs, only audio downloads are skipped.
     */
    suspend fun syncAll() {
        if (syncing) return
        syncing = true
        lastError = null
        try {
            for (playlistId in keptPlaylists.keys.toList()) {
                val detail = try {
                    api.playlist(playlistId)
                } catch (e: com.music.app.api.ApiException) {
                    // Gone or no longer shared with us: stop following it.
                    keptPlaylists.remove(playlistId)
                    removePlaylist(playlistId)
                    continue
                }
                val before = playlistTracks[playlistId].orEmpty().toSet()
                val now = detail.tracks.map { it.id }.toSet()
                if (!waitingForWifi) {
                    for (track in detail.tracks) if (!index.containsKey(track.id)) download(track)
                }
                playlistIndex[playlistId] = detail.toPlaylist()
                playlistTracks = playlistTracks + (playlistId to detail.tracks.map { it.id })
                val stillNeeded = playlistTracks.values.flatten().toSet()
                for (gone in before - now) if (gone !in stillNeeded) remove(gone)
            }
            persistKept()
            persistPlaylists()

            // Metadata delta for everything downloaded.
            val since = store.loadMeta(KEY_SYNC_SINCE)
            val changes = api.trackChanges(since)
            var changed = false
            for (track in changes.tracks) {
                if (index.containsKey(track.id) && index[track.id] != track) {
                    index[track.id] = track
                    changed = true
                }
            }
            for (id in changes.deleted) {
                if (index.containsKey(id)) {
                    store.delete(id)
                    index.remove(id)
                    changed = true
                }
            }
            if (changed) persist()
            store.saveMeta(KEY_SYNC_SINCE, changes.now)
            lastSyncedAt = changes.now
            store.saveMeta(KEY_SYNC_AT, changes.now)
        } catch (e: Exception) {
            lastError = friendlyError(e)
        } finally {
            syncing = false
        }
    }

    private fun persistKept() {
        store.saveMeta(KEY_KEPT, keptPlaylists.keys.joinToString(","))
    }

    // ---------------------------------------------------------------- playlists

    /** Playlists available offline, alphabetical. */
    val downloadedPlaylists: List<Playlist>
        get() = playlistIndex.values.sortedBy { it.name.lowercase() }

    /**
     * True only when every track of the playlist is actually on the device.
     *
     * Derived rather than stored, so it self-corrects: remove one track on its
     * own, or lose a file to the OS, and the playlist goes back to offering a
     * download instead of claiming to be complete. An empty playlist is never
     * "downloaded" — there would be nothing to download.
     */
    fun isPlaylistDownloaded(id: String): Boolean {
        if (!playlistIndex.containsKey(id)) return false
        val trackIds = playlistTracks[id].orEmpty()
        return trackIds.isNotEmpty() && trackIds.all { index.containsKey(it) }
    }


    /**
     * Downloads every track in a playlist and records the playlist itself, so
     * it stays browsable with no connection. Tracks already on the device are
     * skipped, and [progress] reports completed/total as it goes.
     */
    suspend fun downloadPlaylist(
        playlist: Playlist,
        tracks: List<Track>,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        // Nothing to keep offline, and recording it would make an empty
        // playlist claim to be downloaded.
        if (tracks.isEmpty()) return

        lastError = null
        var done = 0
        progress(0, tracks.size)
        for (track in tracks) {
            download(track)
            done++
            progress(done, tracks.size)
        }
        // Record it even if some tracks failed: the ones that made it are
        // playable, and the playlist should reflect what's actually there.
        playlistIndex[playlist.id] = playlist
        playlistTracks = playlistTracks + (playlist.id to tracks.map { it.id })
        persistPlaylists()
    }

    /**
     * Forgets a downloaded playlist. Audio is removed only for tracks that no
     * other downloaded playlist needs and that weren't downloaded on their own,
     * so removing one playlist never silently deletes another's music.
     */
    fun removePlaylist(playlistId: String, keepTracks: Boolean = false) {
        val ids = playlistTracks[playlistId].orEmpty()
        playlistIndex.remove(playlistId)
        playlistTracks = playlistTracks - playlistId
        if (keptPlaylists.remove(playlistId) != null) persistKept()

        if (!keepTracks) {
            val stillNeeded = playlistTracks.values.flatten().toSet()
            for (trackId in ids) {
                if (trackId !in stillNeeded) remove(trackId)
            }
        }
        persistPlaylists()
    }

    /** The playlists to show when there is no connection. */
    fun offlinePlaylists(): List<Playlist> = downloadedPlaylists

    /**
     * A downloaded playlist rebuilt for offline viewing: its saved metadata
     * plus the tracks that are actually on the device, in their original order.
     * Returns null when the playlist was never downloaded.
     */
    fun offlinePlaylistDetail(playlistId: String): PlaylistDetail? {
        val playlist = playlistIndex[playlistId] ?: return null
        val ordered = playlistTracks[playlistId].orEmpty().mapNotNull { index[it] }
        return PlaylistDetail(
            id = playlist.id,
            name = playlist.name,
            trackCount = ordered.size,
            totalDuration = ordered.sumOf { it.duration },
            createdAt = playlist.createdAt,
            isPublic = playlist.isPublic,
            owner = playlist.owner,
            isMine = playlist.isMine,
            isPinned = playlist.isPinned,
            tracks = ordered,
        )
    }

    /**
     * Remembers the playlists the server last sent. Only the ones that are
     * downloaded are shown offline, but keeping the metadata current means a
     * renamed playlist doesn't show its old name after going offline.
     */
    fun cachePlaylists(current: List<Playlist>) {
        var changed = false
        for (playlist in current) {
            if (playlistIndex.containsKey(playlist.id) && playlistIndex[playlist.id] != playlist) {
                playlistIndex[playlist.id] = playlist
                changed = true
            }
        }
        if (changed) persistPlaylists()
    }

    private fun persistPlaylists() {
        store.saveMeta(
            KEY_PLAYLISTS,
            runCatching {
                json.encodeToString(ListSerializer(Playlist.serializer()), playlistIndex.values.toList())
            }.getOrNull(),
        )
        store.saveMeta(
            KEY_PLAYLIST_TRACKS,
            runCatching {
                json.encodeToString(
                    MapSerializer(String.serializer(), ListSerializer(String.serializer())),
                    playlistTracks,
                )
            }.getOrNull(),
        )
    }

    private fun reconcilePlaylists() {
        val stored = store.loadMeta(KEY_PLAYLISTS)
            ?.let { runCatching { json.decodeFromString(ListSerializer(Playlist.serializer()), it) }.getOrNull() }
            ?: emptyList()
        playlistIndex.clear()
        for (playlist in stored) playlistIndex[playlist.id] = playlist

        playlistTracks = store.loadMeta(KEY_PLAYLIST_TRACKS)
            ?.let {
                runCatching {
                    json.decodeFromString(
                        MapSerializer(String.serializer(), ListSerializer(String.serializer())),
                        it,
                    )
                }.getOrNull()
            }
            ?: emptyMap()
    }

    fun cacheUser(user: User) {
        store.saveMeta(KEY_USER, runCatching { json.encodeToString(User.serializer(), user) }.getOrNull())
    }

    /** The last signed-in user, so the app can open offline without reaching the server. */
    fun cachedUser(): User? =
        store.loadMeta(KEY_USER)?.let {
            runCatching { json.decodeFromString(User.serializer(), it) }.getOrNull()
        }

    /** Wipes downloads and the cached session — called on sign-out. */
    fun clearForLogout() {
        removeAll()
        playlistIndex.clear()
        playlistTracks = emptyMap()
        keptPlaylists.clear()
        persistKept()
        persistPlaylists()
        store.saveMeta(KEY_USER, null)
        store.saveMeta(KEY_SYNC_SINCE, null)
        store.saveMeta(KEY_SYNC_AT, null)
        lastSyncedAt = null
    }

    // ---------------------------------------------------------------- internals

    /** Loads the persisted index and drops entries whose audio file is gone. */
    private fun reconcile() {
        val stored = store.loadMeta(KEY_INDEX)
            ?.let { runCatching { json.decodeFromString(ListSerializer(Track.serializer()), it) }.getOrNull() }
            ?: emptyList()

        index.clear()
        var pruned = false
        for (track in stored) {
            if (store.pathFor(track.id) != null) {
                index[track.id] = track
            } else {
                pruned = true
            }
        }
        if (pruned) persist() else bytesUsed = store.bytesUsed()
    }

    private fun persist() {
        val payload = runCatching {
            json.encodeToString(ListSerializer(Track.serializer()), index.values.toList())
        }.getOrNull()
        store.saveMeta(KEY_INDEX, payload)
        bytesUsed = store.bytesUsed()
    }

    private companion object {
        const val KEY_INDEX = "downloads.index"
        const val KEY_USER = "downloads.user"
        const val KEY_PLAYLISTS = "downloads.playlists"
        const val KEY_PLAYLIST_TRACKS = "downloads.playlistTracks"
        const val KEY_QUALITY = "downloads.quality"
        const val KEY_WIFI_ONLY = "downloads.wifiOnly"
        const val KEY_KEPT = "downloads.keptPlaylists"
        const val KEY_SYNC_SINCE = "downloads.syncSince"
        const val KEY_SYNC_AT = "downloads.syncAt"
    }
}

/**
 * File extension for the downloaded audio. Both players sniff the container
 * from the extension, so a wrong one can make a valid file refuse to play.
 */
internal fun extensionFor(track: Track, useCompatible: Boolean = prefersCompatible(track)): String {
    // Name the file after the format actually stored: players sniff the
    // container from the extension, so an AAC file called .ogg won't play.
    val effectiveMime = if (useCompatible && track.hasCompatible) {
        track.compatibleMimeType
    } else {
        track.mimeType
    }
    val fromMime = when (effectiveMime.substringBefore(';').trim().lowercase()) {
        "audio/mpeg", "audio/mp3" -> "mp3"
        "audio/flac", "audio/x-flac" -> "flac"
        "audio/wav", "audio/x-wav", "audio/wave" -> "wav"
        "audio/ogg", "application/ogg" -> "ogg"
        "audio/opus" -> "opus"
        "audio/aac" -> "aac"
        "audio/mp4", "audio/m4a", "audio/x-m4a" -> "m4a"
        "audio/webm" -> "webm"
        "audio/aiff", "audio/x-aiff" -> "aiff"
        "audio/x-ms-wma" -> "wma"
        else -> null
    }
    return fromMime ?: "mp3"
}
