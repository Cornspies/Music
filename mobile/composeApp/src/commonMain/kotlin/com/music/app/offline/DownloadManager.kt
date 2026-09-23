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

enum class DownloadQuality {
    ORIGINAL,

    SMALLER,
}

enum class DownloadState { NONE, DOWNLOADING, DONE, FAILED }

class DownloadManager(
    private val api: ApiClient,
    private val store: OfflineStore,
) {
    private val json = Json { ignoreUnknownKeys = true }

    private val index = mutableStateMapOf<String, Track>()

    private val transient = mutableStateMapOf<String, DownloadState>()

    var bytesUsed by mutableStateOf(0L)
        private set

    var lastError by mutableStateOf<String?>(null)
        private set

    var downloadQuality by mutableStateOf(DownloadQuality.ORIGINAL)
        private set

    fun updateDownloadQuality(quality: DownloadQuality) {
        downloadQuality = quality
        store.saveMeta(KEY_QUALITY, quality.name)
    }

    var wifiOnly by mutableStateOf(false)
        private set

    fun updateWifiOnly(on: Boolean) {
        wifiOnly = on
        store.saveMeta(KEY_WIFI_ONLY, if (on) "1" else "0")
    }

    val waitingForWifi: Boolean
        get() = wifiOnly && NetworkMonitor.isMetered()

    var syncing by mutableStateOf(false)
        private set

    var lastSyncedAt by mutableStateOf<String?>(null)
        private set

    fun useCompatibleFor(track: Track): Boolean {
        if (prefersCompatible(track)) return true
        if (!track.hasCompatible) return false
        return when (downloadQuality) {
            DownloadQuality.ORIGINAL -> false
            DownloadQuality.SMALLER ->
                track.compatibleFileSize > 0 && track.compatibleFileSize < track.fileSize
        }
    }

    private val playlistIndex = mutableStateMapOf<String, Playlist>()

    private var playlistTracks: Map<String, List<String>> = emptyMap()

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

    val tracks: List<Track>
        get() = index.values.sortedBy { it.title.lowercase() }

    val count: Int get() = index.size

    fun stateOf(trackId: String): DownloadState = when {
        index.containsKey(trackId) -> DownloadState.DONE
        else -> transient[trackId] ?: DownloadState.NONE
    }

    fun localPathFor(trackId: String): String? =
        if (index.containsKey(trackId)) store.pathFor(trackId) else null

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

    fun clearFailure(trackId: String) {
        if (transient[trackId] == DownloadState.FAILED) transient.remove(trackId)
        lastError = null
    }

    fun refreshMetadata(track: Track) {
        if (index.containsKey(track.id)) {
            index[track.id] = track
            persist()
        }
    }

    fun isKeptOffline(playlistId: String): Boolean = keptPlaylists[playlistId] == true

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

    suspend fun syncAll() {
        if (syncing) return
        syncing = true
        lastError = null
        try {
            for (playlistId in keptPlaylists.keys.toList()) {
                val detail = try {
                    api.playlist(playlistId)
                } catch (_: com.music.app.api.ApiException) {
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

    val downloadedPlaylists: List<Playlist>
        get() = playlistIndex.values.sortedBy { it.name.lowercase() }

    fun isPlaylistDownloaded(id: String): Boolean {
        if (!playlistIndex.containsKey(id)) return false
        val trackIds = playlistTracks[id].orEmpty()
        return trackIds.isNotEmpty() && trackIds.all { index.containsKey(it) }
    }


    suspend fun downloadPlaylist(
        playlist: Playlist,
        tracks: List<Track>,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        if (tracks.isEmpty()) return

        lastError = null
        var done = 0
        progress(0, tracks.size)
        for (track in tracks) {
            download(track)
            done++
            progress(done, tracks.size)
        }
        playlistIndex[playlist.id] = playlist
        playlistTracks = playlistTracks + (playlist.id to tracks.map { it.id })
        persistPlaylists()
    }

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

    fun offlinePlaylists(): List<Playlist> = downloadedPlaylists

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

    fun cachedUser(): User? =
        store.loadMeta(KEY_USER)?.let {
            runCatching { json.decodeFromString(User.serializer(), it) }.getOrNull()
        }

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

internal fun extensionFor(track: Track, useCompatible: Boolean = prefersCompatible(track)): String {
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
