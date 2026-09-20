package com.music.app.state

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.music.app.Screen
import com.music.app.api.ApiClient
import com.music.app.api.AuthConfig
import com.music.app.api.ApiException
import com.music.app.api.DiscoverResult
import com.music.app.api.DiscoverSearch
import com.music.app.api.DiscoverSource
import com.music.app.api.Lyrics
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
import kotlin.random.Random
import kotlin.time.Duration.Companion.milliseconds

/** Repeat modes for the player (off / repeat queue / repeat current track). */
enum class RepeatMode { OFF, ALL, ONE }

/** How often the playback position is written to the server while playing. */
private const val PLAYBACK_SAVE_INTERVAL_MS = 10_000L

/** How often to check whether the media token wants renewing. */
private const val MEDIA_TOKEN_CHECK_INTERVAL_MS = 5L * 60_000L

/** Thrown by login/register when the server URL is plain http off the LAN. */
class InsecureServerException(message: String) : Exception(message)

/**
 * Whether a plain-http server address is somewhere the session token may
 * safely travel unencrypted: this machine, an emulator's host, or a private
 * (RFC 1918 / link-local / ULA) address on the same network.
 */
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

/**
 * The warning to show before signing in to [baseUrl], or null when the
 * address is fine: https anywhere, or http to a private address.
 */
fun insecureServerWarning(baseUrl: String): String? {
    val url = baseUrl.trim()
    if (!url.startsWith("http://", ignoreCase = true)) return null
    val host = url.removePrefix("http://").removePrefix("HTTP://").substringBefore('/').substringBefore(':')
    if (host.isBlank() || isPrivateHost(host)) return null
    return "Plain http:// outside your local network sends your password unencrypted. Use https://."
}

/**
 * The single observable app state holder (ViewModel-lite).
 *
 * Holds: auth/user, the back stack, all loaded collections (tracks / stats / favorites /
 * playlists / playlist detail / search), and the player queue. Data is fetched with
 * coroutines on [scope] (main dispatcher) through [api]; all fields are Compose snapshot
 * state so any @Composable observing them recomposes automatically.
 */
class AppModel(
    val store: TokenStore = TokenStore(),
    val player: PlayerController = PlayerController(),
) {
    val api = ApiClient(store)
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    /** Offline downloads: the on-device library and its download/remove actions. */
    val downloads = DownloadManager(api, OfflineStore())

    /**
     * True when the app started from a cached session because the server
     * couldn't be reached. Downloaded tracks still play; anything needing the
     * server (library, search, upload) will show its own error state.
     */
    var offlineMode by mutableStateOf(false)
        private set

    /** True while the Reconnect button is working. */
    var reconnecting by mutableStateOf(false)
        private set

    // ------------------------------------------------------------ auth

    var user by mutableStateOf<User?>(null)
        private set
    var bootstrapping by mutableStateOf(true)
        private set

    // ------------------------------------------------------------ navigation (simple back stack)

    val backStack = mutableStateListOf<Screen>()

    // ------------------------------------------------------------ library data

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

    /** Playlists other people have shared publicly (excludes your own). */
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

    // ------------------------------------------------------------ player queue

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
        // Player end-of-track → advance the queue on the main thread.
        player.onEnded = { scope.launch { onTrackEnded() } }
        // Lock screen / headset / notification skips: the queue is ours.
        player.onNext = { scope.launch { next() } }
        player.onPrevious = { scope.launch { previous() } }

        // Media URLs carry a short-lived token; keep one in hand while signed
        // in so a track that starts near the end of a token's life can still
        // seek hours later.
        scope.launch {
            while (true) {
                if (user != null && !offlineMode && api.mediaTokenNeedsRefresh) {
                    runCatching { api.refreshMediaToken() }
                }
                delay(MEDIA_TOKEN_CHECK_INTERVAL_MS.milliseconds)
            }
        }

        // Continue listening: save the position every few seconds while
        // something plays, and the moment playback pauses (which also covers
        // the end of a track and the app being sent to the background by a
        // phone call or the like).
        scope.launch {
            while (true) {
                delay(PLAYBACK_SAVE_INTERVAL_MS.milliseconds)
                if (player.isPlaying.value) savePlayback()
            }
        }
        scope.launch {
            player.isPlaying.collect { playing -> if (!playing) savePlayback() }
        }

        // Restore a previous session (the JWT may have expired — verify it).
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
                    // An ApiException means the server answered and rejected us
                    // (expired/invalid token) — that token is genuinely dead.
                    // Anything else is a transport failure: keep the session and
                    // start offline so downloaded music is still playable.
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

    // ------------------------------------------------------------ auth actions

    /**
     * @throws InsecureServerException when [baseUrl] is plain http off the
     *   LAN and [acceptInsecure] is false; the screen shows the warning.
     * @throws Exception on network / credential problems (screens handle the message).
     */
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

    /**
     * Sign-up policy, loaded when the auth screen appears. Falls back to
     * permissive defaults if the server is older or unreachable, so the form
     * still renders.
     */
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

    /**
     * Replaces the signed-in user's avatar. The library is refreshed afterwards
     * because every track carries its uploader's avatar flag, so rows for this
     * user's tracks need to pick up the change.
     * @throws Exception on network / server problems (the caller shows it).
     */
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

    /** Removes the signed-in user's avatar. @throws Exception on failure. */
    suspend fun clearAvatar() {
        val updated = api.deleteAvatar()
        user = updated
        downloads.cacheUser(updated)
        loadLibrary()
    }

    /**
     * Signs out on every device by revoking all of this account's sessions on
     * the server, then locally. @throws Exception when the server can't be
     * reached — nothing is forgotten locally in that case.
     */
    suspend fun logoutEverywhere() {
        api.logoutEverywhere()
        logout()
    }

    fun logout() {
        store.saveToken(null)
        api.clearMediaToken()
        // Downloads belong to the signed-in account, so they go with the session —
        // otherwise the next user on this device inherits the previous one's music.
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
            // The media token goes into every cover and stream URL, so fetch
            // it before the library renders any. Offline, the fallback is the
            // session token — and downloaded tracks don't need one at all.
            if (!offlineMode) runCatching { api.refreshMediaToken() }
            refreshAll()
            restorePlayback()
            // Kept-offline playlists follow the server; do it after the
            // library is up so the first screen isn't waiting on downloads.
            if (!offlineMode) downloads.syncAll()
        }
    }

    fun refreshAll() {
        scope.launch { loadLibraryInternal(tracksSort) }
        scope.launch { loadStatsInternal() }
        scope.launch { loadFavoritesInternal() }
        scope.launch { loadPlaylistsInternal() }
    }

    // ------------------------------------------------------------ navigation actions

    fun push(screen: Screen) {
        if (backStack.lastOrNull() != screen) backStack.add(screen)
    }

    fun pop() {
        if (backStack.size > 1) backStack.removeAt(backStack.size - 1)
    }

    /** Bottom tab tap — reset the stack so "back" never crosses tabs. */
    fun selectTab(screen: Screen) {
        if (backStack.size == 1 && backStack[0] == screen) return
        backStack.clear()
        backStack.add(screen)
    }

    // ------------------------------------------------------------ data loaders

    fun loadLibrary(sort: String = tracksSort) {
        tracksSort = sort
        scope.launch { loadLibraryInternal(sort) }
    }

    private suspend fun loadLibraryInternal(sort: String) {
        tracksLoading = true
        tracksError = null
        try {
            tracks = api.tracks(sort = sort)
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                // Offline: show what's on the device rather than an error page.
                tracks = downloads.tracks
                tracksError = null
            } else {
                tracksError = friendlyError(e)
            }
        } finally {
            tracksLoading = false
        }
    }

    /**
     * Records that the server answered. Clears offline mode so the banner and
     * the offline fallbacks go away as soon as anything succeeds.
     */
    private fun noteOnline() {
        if (offlineMode) offlineMode = false
    }

    /**
     * Classifies a failed request. Returns true when it looks like a lost
     * connection rather than a real server answer, in which case callers fall
     * back to downloaded content instead of surfacing an error.
     *
     * An ApiException means the server replied (4xx/5xx) — that is a genuine
     * error and must not be disguised as "offline".
     */
    private fun noteFailure(e: Exception): Boolean {
        if (e is ApiException) return false
        offlineMode = true
        return true
    }

    /**
     * Retry the connection after a failure — the Reconnect button. Re-checks
     * the session too, so a token refreshed while offline is picked up.
     */
    fun reconnect() {
        scope.launch {
            reconnecting = true
            try {
                user = api.me()
                downloads.cacheUser(user!!)
                offlineMode = false
                refreshAll()
            } catch (e: Exception) {
                // The server answering with a rejection means the session is
                // genuinely over — reconnecting can't fix that.
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
                // Offline: describe the on-device library instead of an error,
                // so Home still shows meaningful numbers.
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
            favorites = api.favorites()
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                // Favourites aren't cached; offline we simply show the ones
                // that happen to be downloaded rather than an error.
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
            // /api/playlists already returns your own plus anything you pinned;
            // the public list is what's available to pin.
            playlists = api.playlists()
            publicPlaylists = runCatching { api.publicPlaylists() }.getOrDefault(emptyList())
            downloads.cachePlaylists(playlists ?: emptyList())
            noteOnline()
        } catch (e: Exception) {
            if (noteFailure(e)) {
                // Offline: the playlists you downloaded, plus the last list the
                // server sent, so the screen still looks like itself.
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

    /**
     * Pin or unpin someone else's public playlist. The lists are refetched
     * because pinning adds an entry that wasn't in `playlists` at all.
     * @throws Exception on failure, for the caller to surface.
     */
    suspend fun setPlaylistPinned(id: String, pinned: Boolean) {
        if (pinned) api.pinPlaylist(id) else api.unpinPlaylist(id)
        loadPlaylistsInternal()
        if (playlistDetail?.id == id) loadPlaylistInternal(id)
    }

    /** Share or unshare one of your own playlists. @throws Exception on failure. */
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
                // Offline and not downloaded: say so plainly rather than
                // showing a generic network error.
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

    /**
     * Opens a playlist that arrived as a share link. Resets the back stack to
     * Home first so "back" leaves the playlist and lands somewhere sensible,
     * rather than exiting the app.
     */
    /**
     * Downloads a playlist from a screen that only has its summary — the
     * overview list has no track data, so the detail is fetched first.
     * @throws Exception on failure, for the caller to surface.
     */
    suspend fun downloadPlaylistById(
        playlist: Playlist,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ) {
        val detail = api.playlist(playlist.id)
        downloads.downloadPlaylist(detail.toPlaylist(), detail.tracks, progress)
    }

    /**
     * Deletes a track and drops it from every list in memory.
     * Only the uploader may delete; the server answers 403 otherwise.
     * @throws Exception on failure, for the caller to surface.
     */
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
        // The audio is gone from the server, so a downloaded copy is stale.
        downloads.remove(track.id)
        loadStats()
    }

    fun openSharedPlaylist(id: String) {
        backStack.clear()
        backStack.add(Screen.Home)
        backStack.add(Screen.PlaylistDetail(id))
        loadPlaylist(id)
    }

    /** Debounced by SearchScreen (350 ms) before calling this. */
    fun search(query: String) {
        scope.launch {
            searchLoading = true
            try {
                searchResults = api.search(query.trim())
            } catch (_: Exception) {
                // Keep previous results; transient search failures are non-fatal.
            } finally {
                searchLoading = false
            }
        }
    }

    fun clearSearch() {
        searchResults = null
    }

    // ------------------------------------------------------------ discover

    /** Catalogues the server can search; null until asked, empty when off. */
    var discoverSources by mutableStateOf<List<DiscoverSource>?>(null)
        private set
    var discoverResults by mutableStateOf<DiscoverSearch?>(null)
        private set
    var discoverLoading by mutableStateOf(false)
        private set
    var discoverError by mutableStateOf<String?>(null)
        private set
    private var discoverQuery: Pair<String, String>? = null // source to query

    fun loadDiscoverSources() {
        if (discoverSources != null) return
        scope.launch {
            discoverSources = runCatching { api.discoverSources() }.getOrDefault(emptyList())
        }
    }

    /** Debounced by SearchScreen, like [search]. */
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
            } catch (_: Exception) {
                // Keep what's shown; "load more" can be tapped again.
            } finally {
                discoverLoading = false
            }
        }
    }

    fun clearDiscover() {
        discoverQuery = null
        discoverResults = null
        discoverError = null
    }

    /** Adds one result to the library and marks it in the shown results. */
    suspend fun discoverAdd(result: DiscoverResult): Track {
        val track = api.discoverAdd(result.source, result.id)
        discoverResults = discoverResults?.let { r ->
            r.copy(results = r.results.map { if (it.id == result.id) it.copy(trackId = track.id) else it })
        }
        loadLibrary()
        return track
    }

    // ------------------------------------------------------------ playlist mutations

    /** @throws Exception with the server message (e.g. name too long). */
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

    /** @throws Exception (e.g. 409 track already in playlist). */
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

    // ------------------------------------------------------------ playback

    /** Play [track] now, using [context] as the "up next" queue. */
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
        scope.launch {
            try {
                api.reportPlay(track.id)
                bumpPlays(track.id)
            } catch (_: Exception) {
                // Play counter is best-effort.
            }
        }
        savePlayback()
    }

    /** Puts queue[queueIndex] into the player; returns it, or null if the queue is empty. */
    private fun loadCurrent(autoplay: Boolean): Track? {
        val track = queue.getOrNull(queueIndex) ?: return null
        currentTrack = track
        prefetchNeighbourWaveforms()
        // Downloaded copy wins over streaming: instant start, no data used, and
        // it's the only thing that works with no connection.
        // The Track overload asks for the compatible copy when this platform
        // cannot decode the original (Ogg on iOS).
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

    // ------------------------------------------------------------ waveform

    /**
     * Peaks by track id. Kept for the whole session (200 ints a track) so
     * skipping back to something already heard shows its waveform at once.
     */
    private val waveforms = mutableStateMapOf<String, List<Int>>()

    /** Ids a fetch is already in flight for, so neighbours aren't requested twice. */
    private val waveformRequests = mutableSetOf<String>()

    /** Peaks for [track]'s seek bar, or null while loading / unavailable. */
    fun waveformFor(track: Track): List<Int>? = waveforms[track.id]

    /** Fetches the waveform for [track] if the server has one and it isn't cached yet. */
    fun loadWaveform(track: Track) {
        if (!track.hasWaveform || offlineMode) return
        if (track.id in waveforms || !waveformRequests.add(track.id)) return
        scope.launch {
            try {
                runCatching { api.waveform(track.id) }.getOrNull()?.let { waveforms[track.id] = it }
            } finally {
                waveformRequests.remove(track.id)
            }
        }
    }

    // ------------------------------------------------------------ lyrics

    /** Lyrics by track id, including misses, for the session. */
    private val lyrics = mutableStateMapOf<String, Lyrics>()
    private val lyricsRequests = mutableSetOf<String>()

    /** Lyrics for [track], or null while they haven't been fetched yet. */
    fun lyricsFor(track: Track): Lyrics? = lyrics[track.id]

    fun loadLyrics(track: Track) {
        if (offlineMode || track.id in lyrics || !lyricsRequests.add(track.id)) return
        scope.launch {
            try {
                runCatching { api.lyrics(track.id) }.getOrNull()?.let { lyrics[track.id] = it }
            } finally {
                lyricsRequests.remove(track.id)
            }
        }
    }

    /**
     * Warms the cache for the tracks either side of the current one, so a
     * skip lands on a seek bar that's already a waveform instead of flashing
     * the plain slider first. (Shuffle picks are unpredictable; those still
     * load on arrival.)
     */
    private fun prefetchNeighbourWaveforms() {
        if (queue.isEmpty()) return
        listOf(queueIndex, queueIndex + 1, queueIndex - 1)
            .map { (it + queue.size) % queue.size }
            .distinct()
            .mapNotNull { queue.getOrNull(it) }
            .forEach(::loadWaveform)
    }


    fun togglePlayPause() {
        if (player.isPlaying.value) player.pause() else player.play()
    }

    /** Manual "next" (button): wraps around at the end of the queue. */
    fun next() = advance(auto = false)

    /** Auto-advance (end of track): respects repeat mode, stops at the end otherwise. */
    private fun advance(auto: Boolean) {
        if (queue.isEmpty()) return
        val i = if (shuffle && queue.size > 1) randomOtherIndex() else queueIndex + 1
        when {
            i < queue.size -> {
                queueIndex = i
                startCurrent()
            }
            !auto -> { // manual next on the last track → wrap to the first
                queueIndex = 0
                startCurrent()
            }
            repeatMode == RepeatMode.ALL -> {
                queueIndex = 0
                startCurrent()
            }
            else -> { // end of queue, no repeat → stop, keep the track loaded
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

    fun previous() {
        if (queue.isEmpty()) return
        if (player.positionSec.value > 3.0) {
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

    // ------------------------------------------------------------ continue listening
    //
    // The server remembers what each user last had loaded — track, position and
    // the up-next queue — so the app can pick up where they left off after
    // being closed, or on another device. Restored paused: an app opened by
    // accident shouldn't start making noise.

    private var lastSavedQueueIds: List<String> = emptyList()

    private fun savePlayback() {
        val track = currentTrack ?: return
        if (user == null || offlineMode) return
        val ids = queue.map { it.id }
        val queueChanged = ids != lastSavedQueueIds
        val position = player.positionSec.value
        scope.launch {
            try {
                api.savePlayback(track.id, position, if (queueChanged) ids else null)
                if (queueChanged) lastSavedQueueIds = ids
            } catch (_: Exception) {
                // Best-effort: the next save carries the same information.
            }
        }
    }

    private suspend fun restorePlayback() {
        if (currentTrack != null || offlineMode) return
        val state = try {
            api.playbackState()
        } catch (_: Exception) {
            null
        } ?: return
        // The user may have started something while the request was in flight.
        if (currentTrack != null) return
        queue = state.queue
        queueIndex = state.queueIndex
        // What we just loaded is exactly what the server has.
        lastSavedQueueIds = state.queue.map { it.id }
        loadCurrent(autoplay = false) ?: return
        player.seekTo(state.position)
    }

    private fun onTrackEnded() {
        when (repeatMode) {
            RepeatMode.ONE -> {
                player.seekTo(0.0)
                player.play()
            }
            RepeatMode.OFF, RepeatMode.ALL -> advance(auto = true)
        }
    }

    // ------------------------------------------------------------ favorites

    /** Optimistic toggle, reverted on failure. Updates every loaded collection. */
    fun toggleFavorite(track: Track) {
        val newValue = !track.isFavorite
        val updated = track.copy(isFavorite = newValue)
        replaceTrack(updated)
        // replaceTrack only rewrites entries already in a list, so the
        // favourites list needs the add and remove done explicitly — otherwise
        // a newly favourited track doesn't show up until the app restarts.
        favorites = if (newValue) {
            // The server returns favourites newest-first, so match that here.
            val rest = favorites?.filterNot { it.id == track.id } ?: emptyList()
            listOf(updated) + rest
        } else {
            favorites?.filterNot { it.id == track.id }
        }
        scope.launch {
            try {
                if (newValue) api.addFavorite(track.id) else api.removeFavorite(track.id)
            } catch (_: Exception) {
                // Put it back the way it was, including the favourites list.
                replaceTrack(track)
                loadFavoritesInternal()
            }
        }
    }

    /** Swap [updated] into every collection that contains the track (by id). */
    private fun replaceTrack(updated: Track) {
        fun List<Track>?.withUpdated(): List<Track>? =
            this?.map { if (it.id == updated.id) updated else it }

        tracks = tracks.withUpdated()
        favorites = favorites.withUpdated()
        searchResults = searchResults?.let { it.copy(tracks = it.tracks.withUpdated() ?: emptyList()) }
        playlistDetail = playlistDetail?.let { it.copy(tracks = it.tracks.withUpdated() ?: emptyList()) }
        stats = stats?.let { it.copy(mostPlayed = it.mostPlayed.withUpdated() ?: emptyList()) }
        if (currentTrack?.id == updated.id) currentTrack = updated
    }

    private fun bumpPlays(trackId: String) {
        val source = (tracks ?: emptyList()).firstOrNull { it.id == trackId }
            ?: (favorites ?: emptyList()).firstOrNull { it.id == trackId }
            ?: currentTrack?.takeIf { it.id == trackId }
            ?: return
        replaceTrack(source.copy(plays = source.plays + 1))
    }

    // ------------------------------------------------------------ lifecycle

    fun destroy() {
        scope.cancel()
        player.release()
    }
}
