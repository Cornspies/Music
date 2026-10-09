package com.music.app.offline

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.music.app.api.ApiClient
import com.music.app.api.Lyrics
import com.music.app.api.Playlist
import com.music.app.api.PlaylistDetail
import com.music.app.api.Track
import com.music.app.api.User
import com.music.app.api.friendlyError
import com.music.app.api.prefersCompatible
import com.music.app.api.toPlaylist
import kotlinx.coroutines.CancellationException
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

    private val staleAudio = mutableSetOf<String>()

    init {
        reconcile()
        reconcilePlaylists()
        downloadQuality = store.loadMeta(KEY_QUALITY)
            ?.let { saved -> DownloadQuality.entries.firstOrNull { it.name == saved } }
            ?: DownloadQuality.ORIGINAL
        wifiOnly = store.loadMeta(KEY_WIFI_ONLY) == "1"
        lastSyncedAt = store.loadMeta(KEY_SYNC_AT)
        store.loadMeta(KEY_KEPT)?.split(',')?.filter { it.isNotBlank() }?.forEach { keptPlaylists[it] = true }
        store.loadMeta(KEY_STALE)?.split(',')?.filter { it.isNotBlank() }?.let { staleAudio.addAll(it) }
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

    fun coverPath(trackId: String): String? =
        if (index.containsKey(trackId)) store.extraPath(trackId, EXTRA_COVER) else null

    fun savedLyrics(trackId: String): Lyrics? {
        if (!index.containsKey(trackId)) return null
        val bytes = store.readExtra(trackId, EXTRA_LYRICS) ?: return null
        return runCatching { json.decodeFromString(Lyrics.serializer(), bytes.decodeToString()) }.getOrNull()
    }

    fun rememberLyrics(trackId: String, lyrics: Lyrics) {
        if (index.containsKey(trackId)) saveLyrics(trackId, lyrics)
    }

    private fun saveLyrics(trackId: String, lyrics: Lyrics) {
        val text = runCatching { json.encodeToString(Lyrics.serializer(), lyrics) }.getOrNull() ?: return
        store.saveExtra(trackId, EXTRA_LYRICS, text.encodeToByteArray())
    }

    private suspend fun saveExtras(track: Track) {
        if (track.hasCover && store.extraPath(track.id, EXTRA_COVER) == null) {
            orNull { api.coverBytes(track.id, COVER_SIZE) }
                ?.takeIf { it.isNotEmpty() }
                ?.let { store.saveExtra(track.id, EXTRA_COVER, it) }
        }
        if (store.extraPath(track.id, EXTRA_LYRICS) == null) {
            orNull { api.lyrics(track.id) }?.let { saveLyrics(track.id, it) }
        }
    }

    private suspend fun fillMissingExtras() {
        if (waitingForWifi) return
        for (track in index.values.toList()) saveExtras(track)
        bytesUsed = store.bytesUsed()
    }

    private suspend fun <T> orNull(block: suspend () -> T): T? =
        try {
            block()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }

    private fun replaceTrack(track: Track) {
        val old = index[track.id]
        if (old?.updatedAt != track.updatedAt) store.deleteExtras(track.id)
        if (old != null && audioChanged(old, track) && staleAudio.add(track.id)) persistStale()
        index[track.id] = track
    }

    private fun audioChanged(old: Track, new: Track): Boolean {
        if (old.fileSize != new.fileSize || old.mimeType != new.mimeType) return true
        if (!useCompatibleFor(old) && !useCompatibleFor(new)) return false
        return old.compatibleFileSize != new.compatibleFileSize
    }

    private fun persistStale() {
        store.saveMeta(KEY_STALE, staleAudio.joinToString(","))
    }

    private suspend fun refreshStaleAudio() {
        for (id in staleAudio.toList()) {
            if (waitingForWifi) return
            val track = index[id]
            if (track == null) {
                staleAudio.remove(id)
                persistStale()
                continue
            }
            download(track, refresh = true)
        }
    }

    suspend fun download(track: Track, useCompatible: Boolean? = null, refresh: Boolean = false) {
        if (index.containsKey(track.id) && !refresh) return
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
            store.save(track.id, extensionFor(track, compatible)) { path ->
                api.downloadTrack(track, compatible, path)
            }
            index[track.id] = track
            if (staleAudio.remove(track.id)) persistStale()
            saveExtras(track)
            transient.remove(track.id)
            persist()
        } catch (e: CancellationException) {
            transient.remove(track.id)
            throw e
        } catch (e: Exception) {
            transient[track.id] = DownloadState.FAILED
            lastError = friendlyError(e)
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

    fun removeTracks(trackIds: Collection<String>) {
        val stillNeeded = playlistTracks.values.flatten().toSet()
        for (id in trackIds) {
            if (id in stillNeeded || !index.containsKey(id)) continue
            store.delete(id)
            index.remove(id)
            transient.remove(id)
        }
        persist()
    }

    fun clearFailure(trackId: String) {
        if (transient[trackId] == DownloadState.FAILED) transient.remove(trackId)
        lastError = null
    }

    fun refreshMetadata(track: Track) {
        if (index.containsKey(track.id) && index[track.id] != track) {
            replaceTrack(track)
            persist()
        }
    }

    fun refreshFrom(tracks: List<Track>) {
        var changed = false
        for (track in tracks) {
            if (index.containsKey(track.id) && index[track.id] != track) {
                replaceTrack(track)
                changed = true
            }
        }
        if (changed) persist()
    }

    suspend fun syncChanges(): List<Track> {
        val since = store.loadMeta(KEY_SYNC_SINCE)
        val changes = api.trackChanges(since)
        var changed = false
        for (track in changes.tracks) {
            if (index.containsKey(track.id) && index[track.id] != track) {
                replaceTrack(track)
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
        refreshStaleAudio()
        return changes.tracks
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

            syncChanges()
            fillMissingExtras()
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


    suspend fun downloadTracks(
        tracks: List<Track>,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        val missing = tracks.filterNot { index.containsKey(it.id) }
        lastError = null
        progress(0, missing.size)
        for ((i, track) in missing.withIndex()) {
            if (waitingForWifi) return
            download(track)
            progress(i + 1, missing.size)
        }
    }

    suspend fun downloadPlaylist(
        playlist: Playlist,
        tracks: List<Track>,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        if (tracks.isEmpty()) return

        downloadTracks(tracks, progress)
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
        const val KEY_STALE = "downloads.staleAudio"
        const val EXTRA_COVER = "cover"
        const val EXTRA_LYRICS = "lyrics"
        const val COVER_SIZE = 512
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
