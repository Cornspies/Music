@file:OptIn(ExperimentalTime::class)

package com.music.app.api

import com.music.app.ui.components.AvatarCrop
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.HttpRequestBuilder
import io.ktor.client.request.delete
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.patch
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.forms.formData
import io.ktor.client.request.forms.submitFormWithBinaryData
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.Headers
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.json.Json
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

/** Thrown when the server responds with a non-2xx status; message comes from `{"error": "..."}`. */
class ApiException(message: String) : Exception(message)

/** Human-readable message for arbitrary failures (network down, bad URL, …). */
fun friendlyError(e: Throwable): String = when (e) {
    is ApiException -> e.message ?: "Request failed"
    else -> "Network error — is the server URL correct and the Music backend running?"
}

/**
 * Ktor client for the Music Go backend (port 3001).
 *
 * JSON requests carry `Authorization: Bearer <JWT>`. Media URLs (streaming / cover art)
 * cannot send headers from platform players, so those URLs embed `?token=<JWT>` instead —
 * the server accepts both (authUser checks the header first, then the query param).
 */
/** Header the server uses to hand back a renewed session token. */
private const val refreshedTokenHeader = "X-Refreshed-Token"

/**
 * Refresh the media token this long before the server's expiry. The server
 * hands out six-hour tokens; refreshing early means a track that starts
 * near the end of one token's life can still seek for hours.
 */
private const val MEDIA_TOKEN_MARGIN_MS = 30L * 60_000L

class ApiClient(private val store: TokenStore) {

    private val json = Json {
        ignoreUnknownKeys = true
    }

    private val client = HttpClient {
        install(ContentNegotiation) {
            json(json)
        }
        install(HttpTimeout) {
            connectTimeoutMillis = 10_000
            // Large multipart uploads (server caps at 250 MB).
            requestTimeoutMillis = 600_000
            // Must be set explicitly. Ktor only forwards this to the engine
            // when it is non-null (`socketTimeoutMillis?.let { readTimeout(..);
            // writeTimeout(..) }`), so leaving it out keeps OkHttp's own
            // defaults of 10s read and 10s *write* — and the write timeout is
            // what kills a large upload part-way through with "Socket timeout
            // has expired".
            //
            // This is an idle timeout, not a total one: it fires only when
            // nothing moves for two minutes, so a slow-but-steady transfer is
            // fine while a genuinely dead connection still fails. The overall
            // cap stays requestTimeoutMillis above.
            socketTimeoutMillis = 120_000
        }
    }

    /**
     * Sliding sessions: once a token is past the halfway point of its life the
     * server returns a replacement in this header. Storing it keeps the session
     * rolling forward, so the app never asks you to sign in again as long as
     * you open it now and then.
     *
     * Marked public only because it is called from an inline function.
     */
    fun noteRefreshedToken(response: HttpResponse) {
        val fresh = response.headers[refreshedTokenHeader]?.trim().orEmpty()
        if (fresh.isNotEmpty() && fresh != store.loadToken()) {
            store.saveToken(fresh)
        }
    }

    /** Server base URL (persisted in [TokenStore]); trailing slashes are trimmed. */
    var baseUrl: String
        get() = store.loadBaseUrl().trim().trimEnd('/')
        set(value) {
            val cleaned = value.trim().trimEnd('/')
            store.saveBaseUrl(cleaned.ifEmpty { defaultBaseUrl })
        }

    private val token: String? get() = store.loadToken()

    // ---------------------------------------------------------------- media token
    //
    // Media URLs carry a short-lived, read-only token rather than the session,
    // so a URL that ends up in a log or a share sheet is worth a few hours of
    // listening rather than the whole account. Held in memory only.

    private var mediaToken: String? = null
    private var mediaTokenExpiresAtMs: Long = 0L

    /** The token to put in a media URL right now; the session as a fallback
     *  until the first media token has been fetched. */
    private val mediaOrSessionToken: String
        get() {
            val m = mediaToken
            return if (m != null && nowMs() < mediaTokenExpiresAtMs) m else (token ?: "")
        }

    /** True when there is no usable media token, or it is about to expire. */
    val mediaTokenNeedsRefresh: Boolean
        get() = mediaToken == null || nowMs() > mediaTokenExpiresAtMs - MEDIA_TOKEN_MARGIN_MS

    /** Fetches a fresh media token. Callers decide when (see AppModel). */
    suspend fun refreshMediaToken() {
        val fresh: MediaToken = getJson("/api/auth/media-token")
        mediaToken = fresh.token
        mediaTokenExpiresAtMs = parseIsoMs(fresh.expiresAt) ?: (nowMs() + 60L * 60_000L)
    }

    fun clearMediaToken() {
        mediaToken = null
        mediaTokenExpiresAtMs = 0L
    }

    // ---------------------------------------------------------------- media URLs

    /** Direct audio stream URL with token query (ExoPlayer / AVPlayer send no headers). */
    fun trackStreamUrl(trackId: String): String =
        url("/api/tracks/$trackId/stream") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    /**
     * Stream URL for a track, asking for the compatible copy when this
     * platform cannot decode the original. The original is always preferred:
     * the copy is a lossy re-encode and only exists so iOS isn't left silent.
     */
    fun trackStreamUrl(track: Track): String {
        val base = trackStreamUrl(track.id)
        return if (prefersCompatible(track)) "$base&version=compatible" else base
    }

    /** Cover art URL with token query (Coil AsyncImage). */
    fun coverUrl(trackId: String): String =
        url("/api/tracks/$trackId/cover") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    /**
     * Thumbnail URL for a metadata suggestion, proxied through the server (Coil
     * AsyncImage). 404s when the candidate has no artwork or its list expired.
     */
    fun candidateArtworkUrl(trackId: String, candidateId: String): String =
        url("/api/tracks/$trackId/metadata/candidates/$candidateId/artwork") +
            "?token=" + mediaOrSessionToken.encodeURLParameter()

    /**
     * Avatar URL with token query (Coil AsyncImage). 404s when the user has no
     * picture, which Coil surfaces as an error state — see UserAvatar.
     */
    fun avatarUrl(userId: String): String =
        url("/api/users/$userId/avatar") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    // ---------------------------------------------------------------- auth

    suspend fun login(username: String, password: String): AuthResponse =
        postJson("/api/auth/login", AuthRequest(username, password))

    suspend fun register(username: String, password: String, inviteCode: String? = null): AuthResponse =
        postJson("/api/auth/register", AuthRequest(username, password, inviteCode))

    /**
     * Sign-up policy for this server. Unauthenticated, and tolerant of older
     * servers that don't have the endpoint — the caller falls back to defaults.
     */
    suspend fun authConfig(): AuthConfig = getJson("/api/auth/config")

    suspend fun me(): User = getJson("/api/auth/me")

    /**
     * Changes the password. Every other session is revoked by the server,
     * which hands back a replacement token for this one; it's stored here so
     * the app stays signed in.
     */
    suspend fun changePassword(current: String, new: String) {
        val response = client.post(url("/api/auth/password")) {
            auth()
            headers.append(HttpHeaders.ContentType, "application/json")
            setBody(ChangePasswordRequest(current, new))
        }
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        if (response.status == HttpStatusCode.NoContent) return // older server
        val fresh = runCatching { response.body<TokenResponse>().token }.getOrNull()
        if (!fresh.isNullOrBlank()) store.saveToken(fresh)
    }

    /** Invalidates every session this account has, on every device. */
    suspend fun logoutEverywhere() {
        expectNoContent(client.post(url("/api/auth/logout-all")) { auth() })
    }

    // ---------------------------------------------------------------- history & sync

    /** Recent listens, newest first; [since] (RFC 3339) narrows the window. */
    suspend fun history(limit: Int = 200, since: String? = null): HistoryResponse =
        getJson("/api/history") {
            parameter("limit", limit)
            if (!since.isNullOrBlank()) parameter("since", since)
        }

    /** Everything changed or deleted since [since] (null for a full listing). */
    suspend fun trackChanges(since: String?): TrackChanges =
        getJson("/api/tracks/changes") {
            if (!since.isNullOrBlank()) parameter("since", since)
        }

    /** Peaks for the seek bar, or null when the track hasn't been analysed. */
    suspend fun waveform(trackId: String): List<Int>? {
        val response = client.get(url("/api/tracks/$trackId/waveform")) { auth() }
        noteRefreshedToken(response)
        if (response.status == HttpStatusCode.NotFound) return null
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body<Waveform>().peaks
    }

    // ---------------------------------------------------------------- tracks

    /** sort: "created" | "title" | "artist" | "plays". */
    suspend fun tracks(sort: String = "created", query: String? = null): List<Track> =
        getJson("/api/tracks") {
            parameter("sort", sort)
            if (!query.isNullOrBlank()) parameter("q", query)
        }

    suspend fun deleteTrack(id: String) {
        expectNoContent(client.delete(url("/api/tracks/$id")) { auth() })
    }

    /** Increments the play counter. Fire-and-forget from the player. */
    suspend fun reportPlay(id: String) {
        expectNoContent(client.post(url("/api/tracks/$id/play")) { auth() })
    }

    /**
     * Multipart upload: `file` part (filename + content type from the original file) and an
     * optional `duration` field in seconds — the server uses it when it cannot parse the
     * duration from tags (e.g. wav files).
     */
    suspend fun uploadTrack(
        fileName: String,
        mimeType: String,
        bytes: ByteArray,
        durationSec: Double?,
    ): Track {
        val safeName = fileName.replace("\"", "'").replace("\r", " ").replace("\n", " ")
        val response = client.submitFormWithBinaryData(
            url = url("/api/tracks"),
            formData = formData {
                append(
                    "file",
                    bytes,
                    Headers.build {
                        append(HttpHeaders.ContentDisposition, "filename=\"$safeName\"")
                        append(HttpHeaders.ContentType, mimeType)
                    },
                )
                if (durationSec != null && durationSec > 0.0 && durationSec < 100_000.0) {
                    append("duration", durationSec.toString())
                }
            },
        ) {
            auth()
        }
        return bodyOrError(response)
    }

    // ---------------------------------------------------------------- avatar

    /** Replaces the signed-in user's avatar; returns the updated profile. */
    suspend fun uploadAvatar(
        fileName: String,
        mimeType: String,
        bytes: ByteArray,
        crop: AvatarCrop? = null,
    ): User {
        val safeName = fileName.replace("\"", "'").replace("\r", " ").replace("\n", " ")
        val response = client.submitFormWithBinaryData(
            url = url("/api/auth/avatar"),
            formData = formData {
                append(
                    "file",
                    bytes,
                    Headers.build {
                        append(HttpHeaders.ContentDisposition, "filename=\"$safeName\"")
                        append(HttpHeaders.ContentType, mimeType)
                    },
                )
                // Fractions of the source image, so they are independent of the
                // size the picture was displayed at while framing it. Omitted
                // entirely when the user didn't crop, which the server reads as
                // "centre crop".
                if (crop != null) {
                    append("cropX", crop.x.toString())
                    append("cropY", crop.y.toString())
                    append("cropSize", crop.size.toString())
                }
            },
        ) {
            auth()
        }
        return bodyOrError(response)
    }

    /** Removes the signed-in user's avatar; returns the updated profile. */
    suspend fun deleteAvatar(): User =
        bodyOrError(client.delete(url("/api/auth/avatar")) { auth() })

    /**
     * Downloads the raw audio for offline playback. Buffered fully in memory —
     * fine for music-sized files, and the server caps uploads at 250 MB.
     */
    suspend fun downloadTrackBytes(trackId: String): ByteArray =
        bodyOrError(client.get(url("/api/tracks/$trackId/download")) { auth() })

    /**
     * Downloads a track for offline use, taking the compatible copy when this
     * platform cannot decode the original — otherwise the file would sit on
     * the device unplayable.
     */
    suspend fun downloadTrackBytes(track: Track, useCompatible: Boolean = prefersCompatible(track)): ByteArray {
        val path = if (useCompatible && track.hasCompatible) {
            "/api/tracks/${track.id}/download?version=compatible"
        } else {
            "/api/tracks/${track.id}/download"
        }
        return bodyOrError(client.get(url(path)) { auth() })
    }

    // ---------------------------------------------------------------- metadata lookup

    /**
     * Online metadata matches for a track, the best first. The server serves candidates
     * found during upload from a short-lived cache and re-searches on a miss, so this
     * also works for tracks uploaded a while ago.
     */
    suspend fun metadataCandidates(trackId: String): MetadataCandidatesResponse =
        getJson("/api/tracks/$trackId/metadata/candidates")

    /** Applies a user-chosen match, overwriting the track's tags; returns the updated track. */
    suspend fun applyMetadataCandidate(trackId: String, candidateId: String): Track =
        postJson("/api/tracks/$trackId/metadata/apply", ApplyMetadataRequest(candidateId))

    // ---------------------------------------------------------------- playback position

    /** Where this user left off, or null when there is nothing to resume (204). */
    suspend fun playbackState(): PlaybackState? {
        val response = client.get(url("/api/playback")) { auth() }
        noteRefreshedToken(response)
        if (response.status == HttpStatusCode.NoContent) return null
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body()
    }

    /**
     * Records where the user is. Pass [queue] (track IDs) only when it changed;
     * null leaves the server's copy alone, keeping the frequent saves tiny.
     */
    suspend fun savePlayback(trackId: String, position: Double, queue: List<String>? = null) {
        expectNoContent(
            client.put(url("/api/playback")) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(PlaybackRequest(trackId, position, queue))
            },
        )
    }

    // ---------------------------------------------------------------- favorites

    suspend fun favorites(): List<Track> = getJson("/api/favorites")

    suspend fun addFavorite(trackId: String) {
        expectNoContent(client.post(url("/api/favorites/$trackId")) { auth() })
    }

    suspend fun removeFavorite(trackId: String) {
        expectNoContent(client.delete(url("/api/favorites/$trackId")) { auth() })
    }

    // ---------------------------------------------------------------- playlists

    suspend fun playlists(): List<Playlist> = getJson("/api/playlists")

    suspend fun createPlaylist(name: String): Playlist =
        postJson("/api/playlists", PlaylistRequest(name))

    suspend fun playlist(id: String): PlaylistDetail = getJson("/api/playlists/$id")

    /** Playlists other users have shared publicly (excludes your own). */
    suspend fun publicPlaylists(): List<Playlist> = getJson("/api/playlists/public")

    /** Share or unshare one of your own playlists. */
    suspend fun setPlaylistPublic(id: String, isPublic: Boolean): Playlist =
        patchJson("/api/playlists/$id", UpdatePlaylistRequest(isPublic = isPublic))

    /** Keep someone else's public playlist in your own list. */
    suspend fun pinPlaylist(id: String): Playlist =
        bodyOrError(client.post(url("/api/playlists/$id/pin")) { auth() })

    /**
     * Remove the pin. The server answers 200 with the playlist, or 204 when it
     * is no longer visible (the owner may have unshared it), so both are fine.
     */
    suspend fun unpinPlaylist(id: String) {
        val response = client.delete(url("/api/playlists/$id/pin")) { auth() }
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
    }

    suspend fun deletePlaylist(id: String) {
        expectNoContent(client.delete(url("/api/playlists/$id")) { auth() })
    }

    suspend fun addPlaylistTrack(playlistId: String, trackId: String) {
        expectNoContent(
            client.post(url("/api/playlists/$playlistId/tracks")) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(AddTrackRequest(trackId))
            },
        )
    }

    suspend fun removePlaylistTrack(playlistId: String, trackId: String) {
        expectNoContent(client.delete(url("/api/playlists/$playlistId/tracks/$trackId")) { auth() })
    }

    // ---------------------------------------------------------------- search & stats

    suspend fun search(query: String): SearchResults = getJson("/api/search") {
        parameter("q", query)
    }

    suspend fun stats(): Stats = getJson("/api/stats")

    // ---------------------------------------------------------------- helpers

    private fun url(path: String): String = baseUrl + path

    private fun nowMs(): Long = Clock.System.now().toEpochMilliseconds()

    private fun parseIsoMs(iso: String): Long? =
        runCatching { Instant.parse(iso).toEpochMilliseconds() }.getOrNull()

    private fun HttpRequestBuilder.auth() {
        val t = token ?: return
        headers.append(HttpHeaders.Authorization, "Bearer $t")
    }

    private suspend inline fun <reified T> getJson(
        path: String,
        block: HttpRequestBuilder.() -> Unit = {},
    ): T = bodyOrError(client.get(url(path)) { auth(); block() })

    private suspend inline fun <reified B, reified T> postJson(path: String, body: B): T =
        bodyOrError(
            client.post(url(path)) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(body)
            },
        )

    private suspend inline fun <reified B, reified T> patchJson(path: String, body: B): T =
        bodyOrError(
            client.patch(url(path)) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(body)
            },
        )

    private suspend inline fun <reified T> bodyOrError(response: HttpResponse): T {
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body()
    }

    private suspend fun expectNoContent(response: HttpResponse) {
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
    }

    private suspend fun apiErrorMessage(response: HttpResponse): String {
        val message = try {
            response.body<ApiError>().error
        } catch (_: Exception) {
            null
        }
        return if (message.isNullOrEmpty()) "Request failed (HTTP ${response.status.value})" else message
    }
}
