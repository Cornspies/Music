package com.music.app.state

import com.music.app.appVersionName
import com.music.app.api.AppRelease
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.music.app.Screen
import com.music.app.DeepLinks
import com.music.app.api.ApiClient
import com.music.app.api.AuthConfig
import com.music.app.api.ApiException
import com.music.app.api.DiscoverResult
import com.music.app.api.DiscoverSearch
import com.music.app.api.DiscoverSource
import com.music.app.api.Lyrics
import com.music.app.api.PlaybackState
import com.music.app.api.PlaylistImport
import com.music.app.api.Playlist
import com.music.app.api.PlaylistDetail
import com.music.app.api.SearchResults
import com.music.app.api.Stats
import com.music.app.ui.components.AvatarCrop
import com.music.app.api.toPlaylist
import com.music.app.api.TokenStore
import com.music.app.api.Track
import com.music.app.api.User
import com.music.app.api.friendlyError
import com.music.app.offline.DownloadManager
import com.music.app.offline.OfflineStore
import com.music.app.player.NowPlayingInfo
import com.music.app.player.PlayerController
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.random.Random
import kotlin.time.Clock
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.Duration.Companion.seconds
import kotlin.time.ExperimentalTime

enum class RepeatMode { OFF, ALL, ONE }

private const val LISTEN_REPORT_INTERVAL_S = 15.0
private const val KEY_SWIPE_TO_DELETE = "ui.swipeToDelete"
private const val KEY_UPDATE_DISMISSED = "update.dismissed"
private const val UPDATE_CHECK_INTERVAL_MS = 6L * 60 * 60 * 1000
private const val CHANGE_CHECK_INTERVAL_MS = 30_000L
private const val PLAYBACK_SAVE_INTERVAL_MS = 10_000L

private const val MEDIA_TOKEN_CHECK_INTERVAL_MS = 5L * 60_000L

class InsecureServerException(message: String) : Exception(message)

fun isPrivateHost(host: String): Boolean {
    val h = host.trim().trimStart('[').trimEnd(']').lowercase()
    if (h == "localhost" || h.endsWith(".local") || h == "::1") return true
    if (h.startsWith("fc") || h.startsWith("fd") || h.startsWith("fe80:")) return true
    val parts = h.split('.')
    if (parts.size != 4 || parts.any { it.toIntOrNull() == null }) return false
    val a = parts[0].toInt()
    val b = parts[1].toInt()
    return a == 10 || a == 127 || (a == 192 && b == 168) || (a == 172 && b in 16..31) || (a == 169 && b == 254)
}

fun insecureServerWarning(baseUrl: String): String? {
    val url = baseUrl.trim()
    if (!url.startsWith("http://", ignoreCase = true)) return null
    val host = url.removePrefix("http://").removePrefix("HTTP://").substringBefore('/').substringBefore(':')
    if (host.isBlank() || isPrivateHost(host)) return null
    return "Plain http:// outside your local network sends your password unencrypted. Use https://."
}

class AppModel(
    val store: TokenStore = TokenStore(),
    val player: PlayerController = PlayerController(),
) {
    val api = ApiClient(store)
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    private val localStore = OfflineStore()

    val downloads = DownloadManager(api, localStore)

    var swipeToDelete by mutableStateOf(localStore.loadMeta(KEY_SWIPE_TO_DELETE) == "1")
        private set

    fun updateSwipeToDelete(on: Boolean) {
        swipeToDelete = on
        localStore.saveMeta(KEY_SWIPE_TO_DELETE, if (on) "1" else null)
    }

    var availableUpdate by mutableStateOf<AppRelease?>(null)
        private set
    var dismissedUpdateVersion by mutableStateOf(localStore.loadMeta(KEY_UPDATE_DISMISSED))
        private set
    private var lastUpdateCheckMs = 0L

    fun checkForUpdate() {
        if (offlineMode || user == null) return
        val current = appVersionName() ?: return
        lastUpdateCheckMs = nowMs()
        scope.launch {
            try {
                val check = api.appUpdate(current)
                availableUpdate = check.latest?.takeIf { check.updateAvailable }
            } catch (_: Exception) {}
        }
    }

    fun dismissUpdate() {
        val version = availableUpdate?.version ?: return
        localStore.saveMeta(KEY_UPDATE_DISMISSED, version)
        dismissedUpdateVersion = version
    }

    fun updateDownloadUrl(): String? = availableUpdate?.let { api.appDownloadUrl(it.fileName) }

    var offlineMode by mutableStateOf(false)
        private set

    var reconnecting by mutableStateOf(false)
        private set

    var user by mutableStateOf<User?>(null)
        private set
    var bootstrapping by mutableStateOf(true)
        private set

    val backStack = mutableStateListOf<Screen>()

    var tracks by mutableStateOf<List<Track>?>(null)
        private set
    var tracksLoading by mutableStateOf(false)
        private set
    var tracksError by mutableStateOf<String?>(null)
        private set
    var tracksSort by mutableStateOf("created")
        private set

    var stats by mutableStateOf<Stats?>(null)
        private set
    var statsLoading by mutableStateOf(false)
        private set
    var statsError by mutableStateOf<String?>(null)
        private set

    var favorites by mutableStateOf<List<Track>?>(null)
        private set
    var favoritesLoading by mutableStateOf(false)
        private set
    var favoritesError by mutableStateOf<String?>(null)
        private set

    var playlists by mutableStateOf<List<Playlist>?>(null)
        private set
    var playlistsLoading by mutableStateOf(false)
        private set
    var playlistsError by mutableStateOf<String?>(null)
        private set

    var publicPlaylists by mutableStateOf<List<Playlist>>(emptyList())
        private set

    var playlistDetail by mutableStateOf<PlaylistDetail?>(null)
        private set
    var playlistDetailLoading by mutableStateOf(false)
        private set
    var playlistDetailError by mutableStateOf<String?>(null)
        private set

    var searchResults by mutableStateOf<SearchResults?>(null)
        private set
    var searchLoading by mutableStateOf(false)
        private set

    var currentTrack by mutableStateOf<Track?>(null)
        private set
    var queue by mutableStateOf<List<Track>>(emptyList())
        private set
    var queueIndex by mutableIntStateOf(0)
        private set

    var shuffle by mutableStateOf(false)
    var repeatMode by mutableStateOf(RepeatMode.OFF)

    init {
        backStack.add(Screen.Home)
        player.onEnded = { scope.launch { onTrackEnded() } }
        player.onNext = { scope.launch { next() } }
        player.onPrevious = { scope.launch { previous() } }

        scope.launch {
            while (true) {
                if (user != null && !offlineMode && api.mediaTokenNeedsRefresh) {
                    runCatching { api.refreshMediaToken() }
                }
                delay(MEDIA_TOKEN_CHECK_INTERVAL_MS.milliseconds)
            }
        }

        scope.launch {
            while (true) {
                delay(PLAYBACK_SAVE_INTERVAL_MS.milliseconds)
                if (player.isPlaying.value) savePlayback()
            }
        }
        scope.launch {
            player.isPlaying.collect { playing -> if (!playing) savePlayback() }
        }

        scope.launch {
            player.positionSec.collect { position ->
                val track = currentTrack ?: return@collect
                if (listeningTrackId != track.id) {
                    flushListening()
                    listeningTrackId = track.id
                    lastListenPosition = null
                }
                lastListenPosition?.let { last ->
                    val delta = position - last
                    if (delta > 0 && delta < 2) unreportedListening += delta
                }
                lastListenPosition = position
                if (unreportedListening >= LISTEN_REPORT_INTERVAL_S) flushListening()
            }
        }
        scope.launch {
            player.isPlaying.collect { playing -> if (!playing) flushListening() }
        }

        val token = store.loadToken()
        if (token.isNullOrBlank()) {
            bootstrapping = false
        } else {
            scope.launch {
                try {
                    user = api.me()
                    offlineMode = false
                    downloads.cacheUser(user!!)
                    onAuthenticated()
                } catch (e: Exception) {
                    val cached = if (e is ApiException) null else downloads.cachedUser()
                    if (cached != null) {
                        user = cached
                        offlineMode = true
                        onAuthenticated()
                    } else {
                        store.saveToken(null)
                    }
                } finally {
                    bootstrapping = false
                }
            }
        }
    }

    suspend fun login(baseUrl: String, username: String, password: String, acceptInsecure: Boolean = false) {
        guardInsecure(baseUrl, acceptInsecure)
        api.baseUrl = baseUrl
        val response = api.login(username.trim(), password)
        store.saveToken(response.token)
        user = response.user
        offlineMode = false
        downloads.cacheUser(response.user)
        onAuthenticated()
    }

    private fun guardInsecure(baseUrl: String, accepted: Boolean) {
        val warning = insecureServerWarning(baseUrl) ?: return
        if (!accepted) throw InsecureServerException(warning)
    }

    var authConfig by mutableStateOf(AuthConfig())
        private set

    fun loadAuthConfig(baseUrl: String) {
        scope.launch {
            api.baseUrl = baseUrl
            authConfig = runCatching { api.authConfig() }.getOrDefault(AuthConfig())
        }
    }

    suspend fun register(
        baseUrl: String,
        username: String,
        password: String,
        inviteCode: String? = null,
        acceptInsecure: Boolean = false,
    ) {
        guardInsecure(baseUrl, acceptInsecure)
        api.baseUrl = baseUrl
        val response = api.register(username.trim(), password, inviteCode?.takeIf { it.isNotBlank() })
        store.saveToken(response.token)
        user = response.user
        offlineMode = false
        downloads.cacheUser(response.user)
        onAuthenticated()
    }

    suspend fun updateAvatar(
        fileName: String,
        mimeType: String,
        bytes: ByteArray,
        crop: AvatarCrop? = null,
    ) {
        val updated = api.uploadAvatar(fileName, mimeType, bytes, crop)
        user = updated
        downloads.cacheUser(updated)
        loadLibrary()
    }

    suspend fun clearAvatar() {
        val updated = api.deleteAvatar()
        user = updated
        downloads.cacheUser(updated)
        loadLibrary()
    }

    suspend fun logoutEverywhere() {
        api.logoutEverywhere()
        logout()
    }

    suspend fun renameAccount(username: String, password: String) {
        user = api.renameAccount(username, password)
    }

    suspend fun deleteAccount(password: String) {
        api.deleteAccount(password)
        logout()
    }

    fun logout() {
        store.saveToken(null)
        api.clearMediaToken()
        downloads.clearForLogout()
        offlineMode = false
        user = null
        player.pause()
        currentTrack = null
        queue = emptyList()
        queueIndex = 0
        lastSavedQueueIds = emptyList()
        backStack.clear()
        backStack.add(Screen.Home)
        tracks = null
        stats = null
        favorites = null
        playlists = null
        publicPlaylists = emptyList()
        playlistDetail = null
        searchResults = null
    }

    private fun onAuthenticated() {
        backStack.clear()
        backStack.add(Screen.Home)
        scope.launch {
            if (!offlineMode) runCatching { api.refreshMediaToken() }
            refreshAll()
            restorePlayback()
            if (!offlineMode) downloads.syncAll()
        }
        checkForUpdate()
    }

    fun refreshAll() {
        scope.launch { loadLibraryInternal(tracksSort) }
        scope.launch { loadStatsInternal() }
        scope.launch { loadFavoritesInternal() }
        scope.launch { loadPlaylistsInternal() }
    }

    fun push(screen: Screen) {
        if (backStack.lastOrNull() != screen) backStack.add(screen)
    }

    fun pop() {
        if (backStack.size > 1) backStack.removeAt(backStack.size - 1)
    }

    fun selectTab(screen: Screen) {
        if (backStack.size == 1 && backStack[0] == screen) return
        backStack.clear()
        backStack.add(screen)
    }

    fun loadLibrary(sort: String = tracksSort) {
        tracksSort = sort
        scope.launch { loadLibraryInternal(sort) }
    }

    private suspend fun loadLibraryInternal(sort: String) {
        tracksLoading = true
        tracksError = null
        try {
            val loaded = api.tracks(sort = sort)
            tracks = loaded
            downloads.refreshFrom(loaded)
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                tracks = downloads.tracks
                tracksError = null
            } else {
                tracksError = friendlyError(e)
            }
        } finally {
            tracksLoading = false
        }
    }

    private fun noteOnline() {
        if (offlineMode) offlineMode = false
    }

    private fun noteFailure(e: Exception): Boolean {
        if (e is ApiException) return false
        offlineMode = true
        return true
    }

    fun reconnect() {
        scope.launch {
            reconnecting = true
            try {
                user = api.me()
                downloads.cacheUser(user!!)
                offlineMode = false
                refreshAll()
            } catch (e: Exception) {
                if (e is ApiException) {
                    logout()
                } else {
                    noteFailure(e)
                }
            } finally {
                reconnecting = false
            }
        }
    }

    fun loadStats() {
        scope.launch { loadStatsInternal() }
    }

    private suspend fun loadStatsInternal() {
        statsLoading = true
        statsError = null
        try {
            stats = api.stats()
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                val offline = downloads.tracks
                stats = Stats(
                    trackCount = offline.size,
                    totalDuration = offline.sumOf { it.duration },
                    artistCount = offline.map { it.artist }.filter { it.isNotBlank() }.distinct().size,
                    albumCount = offline.map { it.album }.filter { it.isNotBlank() }.distinct().size,
                    favoriteCount = offline.count { it.isFavorite },
                    playlistCount = downloads.downloadedPlaylists.size,
                    mostPlayed = offline.sortedByDescending { it.plays }.take(10),
                )
                statsError = null
            } else {
                statsError = friendlyError(e)
            }
        } finally {
            statsLoading = false
        }
    }

    fun loadFavorites() {
        scope.launch { loadFavoritesInternal() }
    }

    private suspend fun loadFavoritesInternal() {
        favoritesLoading = true
        favoritesError = null
        try {
            val loaded = api.favorites()
            favorites = loaded
            downloads.refreshFrom(loaded)
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                favorites = downloads.tracks.filter { it.isFavorite }
                favoritesError = null
            } else {
                favoritesError = friendlyError(e)
            }
        } finally {
            favoritesLoading = false
        }
    }

    fun loadPlaylists() {
        scope.launch { loadPlaylistsInternal() }
    }

    private suspend fun loadPlaylistsInternal() {
        playlistsLoading = true
        playlistsError = null
        try {
            playlists = api.playlists()
            publicPlaylists = runCatching { api.publicPlaylists() }.getOrDefault(emptyList())
            downloads.cachePlaylists(playlists ?: emptyList())
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                playlists = downloads.offlinePlaylists()
                publicPlaylists = emptyList()
                playlistsError = null
            } else {
                playlistsError = friendlyError(e)
            }
        } finally {
            playlistsLoading = false
        }
    }

    suspend fun setPlaylistPinned(id: String, pinned: Boolean) {
        if (pinned) api.pinPlaylist(id) else api.unpinPlaylist(id)
        loadPlaylistsInternal()
        if (playlistDetail?.id == id) loadPlaylistInternal(id)
    }

    suspend fun setPlaylistPublic(id: String, isPublic: Boolean) {
        api.setPlaylistPublic(id, isPublic)
        loadPlaylistsInternal()
        if (playlistDetail?.id == id) loadPlaylistInternal(id)
    }

    fun loadPlaylist(id: String) {
        scope.launch { loadPlaylistInternal(id) }
    }

    private suspend fun loadPlaylistInternal(id: String) {
        playlistDetailLoading = true
        playlistDetailError = null
        try {
            playlistDetail = api.playlist(id)
            noteOnline()
        } catch (e: Exception) {
            val cached = if (noteFailure(e)) downloads.offlinePlaylistDetail(id) else null
            if (cached != null) {
                playlistDetail = cached
                playlistDetailError = null
            } else {
                playlistDetailError = if (offlineMode) {
                    "This playlist isn't downloaded"
                } else {
                    friendlyError(e)
                }
            }
        } finally {
            playlistDetailLoading = false
        }
    }

    suspend fun downloadPlaylistById(
        playlist: Playlist,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        val detail = api.playlist(playlist.id)
        downloads.downloadPlaylist(detail.toPlaylist(), detail.tracks, progress)
    }

    suspend fun deleteTrack(track: Track) {
        api.deleteTrack(track.id)
        tracks = tracks?.filterNot { it.id == track.id }
        favorites = favorites?.filterNot { it.id == track.id }
        playlistDetail = playlistDetail?.let { d ->
            d.copy(tracks = d.tracks.filterNot { it.id == track.id })
        }
        searchResults = searchResults?.let { r ->
            r.copy(tracks = r.tracks.filterNot { it.id == track.id })
        }
        downloads.remove(track.id)
        loadStats()
    }

    suspend fun shareLinkFor(track: Track): Pair<String, String?> {
        val share = api.trackShare(track.id) ?: api.createTrackShare(track.id)
        return DeepLinks.trackShareUrl(api.baseUrl, share.token) to share.expiresAt
    }

    suspend fun stopSharing(track: Track) {
        api.deleteTrackShare(track.id)
    }

    var shareLinkError by mutableStateOf<String?>(null)

    fun openSharedTrack(token: String) {
        scope.launch {
            try {
                val track = api.sharedTrack(token)
                playTrack(track, listOf(track))
            } catch (e: Exception) {
                shareLinkError = friendlyError(e)
            }
        }
    }

    fun openSharedPlaylist(id: String) {
        backStack.clear()
        backStack.add(Screen.Home)
        backStack.add(Screen.PlaylistDetail(id))
        loadPlaylist(id)
    }

    private var lastSearchQuery = ""

    fun search(query: String) {
        lastSearchQuery = query.trim()
        scope.launch {
            searchLoading = true
            try {
                searchResults = api.search(query.trim())
            } catch (_: Exception) {} finally {
                searchLoading = false
            }
        }
    }

    fun searchAllTracks() {
        val query = lastSearchQuery
        if (query.isEmpty()) return
        scope.launch {
            searchLoading = true
            try {
                val all = api.search(query, allTracks = true)
                if (query == lastSearchQuery) searchResults = all
            } catch (_: Exception) {} finally {
                searchLoading = false
            }
        }
    }

    fun clearSearch() {
        searchResults = null
    }

    var discoverSources by mutableStateOf<List<DiscoverSource>?>(null)
        private set
    var discoverResults by mutableStateOf<DiscoverSearch?>(null)
        private set
    var discoverLoading by mutableStateOf(false)
        private set
    var discoverError by mutableStateOf<String?>(null)
        private set
    private var discoverQuery: Pair<String, String>? = null

    fun loadDiscoverSources() {
        if (discoverSources != null) return
        scope.launch {
            discoverSources = runCatching { api.discoverSources() }.getOrDefault(emptyList())
        }
    }

    fun discoverSearch(source: String, query: String) {
        val q = query.trim()
        discoverQuery = source to q
        scope.launch {
            discoverLoading = true
            discoverError = null
            try {
                val r = api.discoverSearch(source, q)
                if (discoverQuery == source to q) discoverResults = r
            } catch (e: Exception) {
                if (discoverQuery == source to q) discoverError = friendlyError(e)
            } finally {
                discoverLoading = false
            }
        }
    }

    fun discoverLoadMore() {
        val (source, q) = discoverQuery ?: return
        val current = discoverResults ?: return
        if (discoverLoading || !current.hasMore) return
        scope.launch {
            discoverLoading = true
            try {
                val next = api.discoverSearch(source, q, current.page + 1)
                if (discoverQuery == source to q) {
                    discoverResults = next.copy(results = current.results + next.results)
                }
            } catch (_: Exception) {} finally {
                discoverLoading = false
            }
        }
    }

    fun clearDiscover() {
        discoverQuery = null
        discoverResults = null
        discoverError = null
    }

    suspend fun discoverAdd(result: DiscoverResult): Track {
        val track = api.discoverAdd(result.source, result.id)
        discoverResults = discoverResults?.let { r ->
            r.copy(results = r.results.map { if (it.id == result.id) it.copy(trackId = track.id) else it })
        }
        loadLibrary()
        return track
    }

    var playlistImports by mutableStateOf<List<PlaylistImport>>(emptyList())
        private set
    private var dismissedImports by mutableStateOf<Set<String>>(emptySet())
    private var importsFinishedSeen = mutableSetOf<String>()
    private var importPolling = false

    val shownImports: List<PlaylistImport> get() = playlistImports.filter { it.id !in dismissedImports }

    fun loadImports() {
        if (importPolling) return
        importPolling = true
        scope.launch {
            try {
                while (true) {
                    val list = try {
                        api.discoverImports()
                    } catch (_: Exception) {
                        delay(10.seconds)
                        continue
                    }
                    playlistImports = list
                    for (job in list) {
                        if (!job.active && importsFinishedSeen.add(job.id)) loadPlaylists()
                    }
                    if (list.none { it.active }) break
                    delay(2.seconds)
                }
            } finally {
                importPolling = false
            }
        }
    }

    suspend fun startImport(source: String, url: String): PlaylistImport {
        val job = api.discoverImport(source, url)
        if (playlistImports.none { it.id == job.id }) playlistImports = listOf(job) + playlistImports
        dismissedImports = dismissedImports - job.id
        loadImports()
        return job
    }

    fun cancelImport(id: String) {
        scope.launch {
            runCatching { api.cancelImport(id) }
            loadImports()
        }
    }

    fun dismissImport(id: String) {
        dismissedImports = dismissedImports + id
    }

    suspend fun createPlaylist(name: String): Playlist {
        val created = api.createPlaylist(name.trim())
        loadPlaylists()
        return created
    }

    fun deletePlaylist(id: String) {
        scope.launch {
            try {
                api.deletePlaylist(id)
                if (playlistDetail?.id == id) {
                    playlistDetail = null
                    if (backStack.lastOrNull() == Screen.PlaylistDetail(id)) pop()
                }
                loadPlaylists()
            } catch (e: Exception) {
                playlistsError = friendlyError(e)
            }
        }
    }

    suspend fun addTrackToPlaylist(playlistId: String, trackId: String) {
        api.addPlaylistTrack(playlistId, trackId)
        loadPlaylistInternal(playlistId)
        loadPlaylists()
    }

    fun removeTrackFromPlaylist(playlistId: String, trackId: String) {
        scope.launch {
            try {
                api.removePlaylistTrack(playlistId, trackId)
                val detail = playlistDetail
                if (detail?.id == playlistId) {
                    playlistDetail = api.playlist(playlistId)
                }
                loadPlaylists()
            } catch (e: Exception) {
                playlistDetailError = friendlyError(e)
            }
        }
    }

    fun playTrack(track: Track, context: List<Track>) {
        val idx = context.indexOfFirst { it.id == track.id }
        queue = if (idx >= 0) context else listOf(track)
        queueIndex = if (idx >= 0) idx else 0
        startCurrent()
    }

    fun playAll(context: List<Track>, index: Int = 0) {
        if (context.isEmpty()) return
        queue = context
        queueIndex = index.coerceIn(0, context.lastIndex)
        startCurrent()
    }

    private fun startCurrent() {
        val track = loadCurrent(autoplay = true) ?: return
        countPlay(track)
        savePlayback()
    }

    private fun countPlay(track: Track) {
        scope.launch {
            try {
                api.reportPlay(track.id)
                bumpPlays(track.id)
            } catch (_: Exception) {}
        }
    }

    private fun loadCurrent(autoplay: Boolean): Track? {
        val track = queue.getOrNull(queueIndex) ?: return null
        currentTrack = track
        prefetchNeighbours()
        val source = downloads.localPathFor(track.id) ?: api.trackStreamUrl(track)
        player.load(
            source,
            autoplay = autoplay,
            info = NowPlayingInfo(
                title = track.title,
                artist = track.artist.ifBlank { "Unknown artist" },
                album = track.album,
                durationSec = track.duration,
                artworkUrl = if (track.hasCover && !offlineMode) api.coverUrl(track.id) else null,
            ),
        )
        return track
    }

    private val waveforms = mutableStateMapOf<String, List<Int>>()

    private val waveformRequests = mutableSetOf<String>()

    private fun waveformKey(track: Track) = "${track.id}:${track.updatedAt}"

    fun waveformFor(track: Track): List<Int>? = waveforms[waveformKey(track)]

    fun loadWaveform(track: Track) {
        if (!track.hasWaveform || offlineMode) return
        val key = waveformKey(track)
        if (key in waveforms || !waveformRequests.add(key)) return
        scope.launch {
            try {
                runCatching { api.waveform(track.id) }.getOrNull()?.let { waveforms[key] = it }
            } finally {
                waveformRequests.remove(key)
            }
        }
    }

    private val lyrics = mutableStateMapOf<String, Lyrics>()
    private val lyricsRequests = mutableSetOf<String>()

    fun lyricsFor(track: Track): Lyrics? = lyrics[track.id]

    private val lyricsMissAt = mutableMapOf<String, Long>()

    @OptIn(ExperimentalTime::class)
    private fun nowMs(): Long = Clock.System.now().toEpochMilliseconds()

    fun loadLyrics(track: Track) {
        if (offlineMode) return
        val cached = lyrics[track.id]
        val missFresh = (lyricsMissAt[track.id] ?: 0L) > nowMs() - 5 * 60_000
        if (cached != null && (cached.found || missFresh)) return
        if (!lyricsRequests.add(track.id)) return
        scope.launch {
            try {
                runCatching { api.lyrics(track.id) }.getOrNull()?.let {
                    lyrics[track.id] = it
                    if (!it.found) lyricsMissAt[track.id] = nowMs()
                }
            } finally {
                lyricsRequests.remove(track.id)
            }
        }
    }

    private fun prefetchNeighbours() {
        if (queue.isEmpty()) return
        listOf(queueIndex, queueIndex + 1, queueIndex - 1)
            .map { (it + queue.size) % queue.size }
            .distinct()
            .mapNotNull { queue.getOrNull(it) }
            .forEach {
                loadWaveform(it)
                loadLyrics(it)
            }
    }


    fun togglePlayPause() {
        if (player.isPlaying.value) player.pause() else player.play()
    }

    fun next() = advance(auto = false)

    private fun advance(auto: Boolean) {
        if (queue.isEmpty()) return
        val i = if (shuffle && queue.size > 1) randomOtherIndex() else queueIndex + 1
        when {
            i < queue.size -> {
                queueIndex = i
                startCurrent()
            }
            !auto -> {
                queueIndex = 0
                startCurrent()
            }
            repeatMode == RepeatMode.ALL -> {
                queueIndex = 0
                startCurrent()
            }
            else -> {
                player.pause()
                player.seekTo(0.0)
            }
        }
    }

    private fun randomOtherIndex(): Int {
        var candidate = queueIndex
        while (candidate == queueIndex) candidate = Random.nextInt(queue.size)
        return candidate
    }

    fun previous(restartFirst: Boolean = true) {
        if (queue.isEmpty()) return
        if (restartFirst && player.positionSec.value > 3.0) {
            player.seekTo(0.0)
            return
        }
        queueIndex = if (queueIndex > 0) queueIndex - 1 else queue.lastIndex
        startCurrent()
    }

    fun seekTo(seconds: Double) {
        player.seekTo(seconds)
        savePlayback()
    }

    private var lastSavedQueueIds: List<String> = emptyList()

    private var knownPlaybackUpdatedAt = ""
    private var syncingPlayback = false

    private fun notePlaybackStamp(stamp: String) {
        if (stamp > knownPlaybackUpdatedAt) knownPlaybackUpdatedAt = stamp
    }

    private var listeningTrackId: String? = null
    private var lastListenPosition: Double? = null
    private var unreportedListening = 0.0

    private fun flushListening() {
        val id = listeningTrackId ?: return
        if (unreportedListening < 0.5 || user == null) return
        val seconds = unreportedListening
        unreportedListening = 0.0
        scope.launch {
            runCatching { api.reportListened(id, seconds) }
        }
    }

    private fun savePlayback() {
        val track = currentTrack ?: return
        if (user == null || offlineMode) return
        val ids = queue.map { it.id }
        val queueChanged = ids != lastSavedQueueIds
        val position = player.positionSec.value
        scope.launch {
            try {
                notePlaybackStamp(api.savePlayback(track.id, position, if (queueChanged) ids else null))
                if (queueChanged) lastSavedQueueIds = ids
            } catch (_: Exception) {}
        }
    }

    private suspend fun restorePlayback() {
        if (currentTrack != null || offlineMode) return
        val state = try {
            api.playbackState()
        } catch (_: Exception) {
            null
        } ?: return
        if (currentTrack != null) return
        applyServerPlayback(state)
    }

    private var lastChangeCheckMs = 0L

    private suspend fun applyServerChanges() {
        if (downloads.syncing) return
        val changed = try {
            downloads.syncChanges()
        } catch (_: Exception) {
            return
        }
        val favoriteIds = favorites?.map { it.id }?.toSet()
        replaceTracks(changed)
        if (favoriteIds != null && changed.any { (it.id in favoriteIds) != it.isFavorite }) loadFavoritesInternal()
    }

    fun onForeground() {
        if (nowMs() - lastUpdateCheckMs > UPDATE_CHECK_INTERVAL_MS) checkForUpdate()
        if (user != null && !offlineMode && nowMs() - lastChangeCheckMs > CHANGE_CHECK_INTERVAL_MS) {
            lastChangeCheckMs = nowMs()
            scope.launch { applyServerChanges() }
        }
        if (user == null || offlineMode || syncingPlayback || player.isPlaying.value) return
        syncingPlayback = true
        scope.launch {
            try {
                val state = api.playbackState(knownPlaybackUpdatedAt.ifBlank { null })
                if (state != null && !player.isPlaying.value) applyServerPlayback(state)
            } catch (_: Exception) {} finally {
                syncingPlayback = false
            }
        }
    }

    private fun applyServerPlayback(state: PlaybackState) {
        val same = currentTrack?.id == state.track.id &&
            abs(player.positionSec.value - state.position) < 1 &&
            queue.map { it.id } == state.queue.map { it.id }
        lastSavedQueueIds = state.queue.map { it.id }
        notePlaybackStamp(state.updatedAt)
        if (same) return
        queue = state.queue
        queueIndex = state.queueIndex
        loadCurrent(autoplay = false) ?: return
        player.seekTo(state.position)
    }

    private fun onTrackEnded() {
        when (repeatMode) {
            RepeatMode.ONE -> {
                flushListening()
                player.seekTo(0.0)
                player.play()
                currentTrack?.let(::countPlay)
            }
            RepeatMode.OFF, RepeatMode.ALL -> advance(auto = true)
        }
    }

    fun toggleFavorite(track: Track) {
        val newValue = !track.isFavorite
        val updated = track.copy(isFavorite = newValue)
        replaceTrack(updated)
        favorites = if (newValue) {
            val rest = favorites?.filterNot { it.id == track.id } ?: emptyList()
            listOf(updated) + rest
        } else {
            favorites?.filterNot { it.id == track.id }
        }
        scope.launch {
            try {
                if (newValue) api.addFavorite(track.id) else api.removeFavorite(track.id)
            } catch (_: Exception) {
                replaceTrack(track)
                loadFavoritesInternal()
            }
        }
    }

    private fun replaceTrack(updated: Track) = replaceTracks(listOf(updated))

    private fun replaceTracks(updated: List<Track>) {
        if (updated.isEmpty()) return
        val byId = updated.associateBy { it.id }
        fun List<Track>?.withUpdated(): List<Track>? =
            this?.map { byId[it.id] ?: it }

        tracks = tracks.withUpdated()
        favorites = favorites.withUpdated()
        searchResults = searchResults?.let { it.copy(tracks = it.tracks.withUpdated() ?: emptyList()) }
        playlistDetail = playlistDetail?.let { it.copy(tracks = it.tracks.withUpdated() ?: emptyList()) }
        stats = stats?.let { it.copy(mostPlayed = it.mostPlayed.withUpdated() ?: emptyList()) }
        currentTrack?.let { current -> byId[current.id]?.let { currentTrack = it } }
        downloads.refreshFrom(updated)
    }

    private fun bumpPlays(trackId: String) {
        val source = (tracks ?: emptyList()).firstOrNull { it.id == trackId }
            ?: (favorites ?: emptyList()).firstOrNull { it.id == trackId }
            ?: currentTrack?.takeIf { it.id == trackId }
            ?: return
        replaceTrack(source.copy(plays = source.plays + 1))
    }

    fun destroy() {
        scope.cancel()
        player.release()
    }
}
