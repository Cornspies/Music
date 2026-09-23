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
import io.ktor.client.request.header
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

class ApiException(message: String) : Exception(message)

class DuplicateException(val response: DuplicateResponse) : Exception(response.error)

fun friendlyError(e: Throwable): String = when (e) {
    is ApiException -> e.message ?: "Request failed"
    else -> "Network error — is the server URL correct and the Music backend running?"
}

private const val refreshedTokenHeader = "X-Refreshed-Token"

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
            requestTimeoutMillis = 600_000
            socketTimeoutMillis = 120_000
        }
    }

    fun noteRefreshedToken(response: HttpResponse) {
        val fresh = response.headers[refreshedTokenHeader]?.trim().orEmpty()
        if (fresh.isNotEmpty() && fresh != store.loadToken()) {
            store.saveToken(fresh)
        }
    }

    var baseUrl: String
        get() = store.loadBaseUrl().trim().trimEnd('/')
        set(value) {
            val cleaned = value.trim().trimEnd('/')
            store.saveBaseUrl(cleaned.ifEmpty { defaultBaseUrl })
        }

    private val token: String? get() = store.loadToken()

    private var mediaToken: String? = null
    private var mediaTokenExpiresAtMs: Long = 0L

    private val mediaOrSessionToken: String
        get() {
            val m = mediaToken
            return if (m != null && nowMs() < mediaTokenExpiresAtMs) m else (token ?: "")
        }

    val mediaTokenNeedsRefresh: Boolean
        get() = mediaToken == null || nowMs() > mediaTokenExpiresAtMs - MEDIA_TOKEN_MARGIN_MS

    suspend fun refreshMediaToken() {
        val fresh: MediaToken = getJson("/api/auth/media-token")
        mediaToken = fresh.token
        mediaTokenExpiresAtMs = parseIsoMs(fresh.expiresAt) ?: (nowMs() + 60L * 60_000L)
    }

    fun clearMediaToken() {
        mediaToken = null
        mediaTokenExpiresAtMs = 0L
    }

    fun trackStreamUrl(trackId: String): String =
        url("/api/tracks/$trackId/stream") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    fun trackStreamUrl(track: Track): String {
        val base = trackStreamUrl(track.id)
        return if (prefersCompatible(track)) "$base&version=compatible" else base
    }

    fun coverUrl(trackId: String): String =
        url("/api/tracks/$trackId/cover") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    fun candidateArtworkUrl(trackId: String, candidateId: String): String =
        url("/api/tracks/$trackId/metadata/candidates/$candidateId/artwork") +
            "?token=" + mediaOrSessionToken.encodeURLParameter()

    fun avatarUrl(userId: String): String =
        url("/api/users/$userId/avatar") + "?token=" + mediaOrSessionToken.encodeURLParameter()

    suspend fun login(username: String, password: String): AuthResponse =
        postJson("/api/auth/login", AuthRequest(username, password))

    suspend fun register(username: String, password: String, inviteCode: String? = null): AuthResponse =
        postJson("/api/auth/register", AuthRequest(username, password, inviteCode))

    suspend fun authConfig(): AuthConfig = getJson("/api/auth/config")

    suspend fun me(): User = getJson("/api/auth/me")

    suspend fun changePassword(current: String, new: String) {
        val response = client.post(url("/api/auth/password")) {
            auth()
            headers.append(HttpHeaders.ContentType, "application/json")
            setBody(ChangePasswordRequest(current, new))
        }
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        if (response.status == HttpStatusCode.NoContent) return
        val fresh = runCatching { response.body<TokenResponse>().token }.getOrNull()
        if (!fresh.isNullOrBlank()) store.saveToken(fresh)
    }

    suspend fun renameAccount(username: String, password: String): User {
        val response = client.post(url("/api/auth/username")) {
            auth()
            headers.append(HttpHeaders.ContentType, "application/json")
            setBody(RenameAccountRequest(username, password))
        }
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        val body = response.body<AuthResponse>()
        store.saveToken(body.token)
        return body.user
    }

    suspend fun deleteAccount(password: String) {
        expectNoContent(
            client.post(url("/api/auth/delete-account")) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(DeleteAccountRequest(password))
            },
        )
    }

    suspend fun logoutEverywhere() {
        expectNoContent(client.post(url("/api/auth/logout-all")) { auth() })
    }

    suspend fun history(limit: Int = 200, since: String? = null): HistoryResponse =
        getJson("/api/history") {
            parameter("limit", limit)
            if (!since.isNullOrBlank()) parameter("since", since)
        }

    suspend fun trackChanges(since: String?): TrackChanges =
        getJson("/api/tracks/changes") {
            if (!since.isNullOrBlank()) parameter("since", since)
        }

    suspend fun waveform(trackId: String): List<Int>? {
        val response = client.get(url("/api/tracks/$trackId/waveform")) { auth() }
        noteRefreshedToken(response)
        if (response.status == HttpStatusCode.NotFound) return null
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body<Waveform>().peaks
    }

    suspend fun lyrics(trackId: String): Lyrics {
        val response = client.get(url("/api/tracks/$trackId/lyrics")) { auth() }
        noteRefreshedToken(response)
        if (response.status == HttpStatusCode.NotFound) {
            return runCatching { response.body<Lyrics>() }.getOrDefault(Lyrics())
        }
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body<Lyrics>()
    }

    suspend fun tracks(sort: String = "created", query: String? = null): List<Track> =
        getJson("/api/tracks") {
            parameter("sort", sort)
            if (!query.isNullOrBlank()) parameter("q", query)
        }

    suspend fun deleteTrack(id: String) {
        expectNoContent(client.delete(url("/api/tracks/$id")) { auth() })
    }

    suspend fun reportPlay(id: String) {
        expectNoContent(client.post(url("/api/tracks/$id/play")) { auth() })
    }

    suspend fun reportListened(id: String, seconds: Double) {
        expectNoContent(
            client.post(url("/api/tracks/$id/listened")) {
                auth()
                headers.append(HttpHeaders.ContentType, "application/json")
                setBody(ListenedRequest(seconds))
            },
        )
    }

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
        if (response.status == HttpStatusCode.Conflict) {
            noteRefreshedToken(response)
            throw DuplicateException(response.body<DuplicateResponse>())
        }
        return bodyOrError(response)
    }

    suspend fun confirmPendingUpload(pendingId: String): Track =
        bodyOrError(client.post(url("/api/uploads/pending/$pendingId")) { auth() })

    suspend fun discardPendingUpload(pendingId: String) {
        expectNoContent(client.delete(url("/api/uploads/pending/$pendingId")) { auth() })
    }

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

    suspend fun deleteAvatar(): User =
        bodyOrError(client.delete(url("/api/auth/avatar")) { auth() })

    suspend fun downloadTrackBytes(trackId: String): ByteArray =
        bodyOrError(client.get(url("/api/tracks/$trackId/download")) { auth() })

    suspend fun downloadTrackBytes(track: Track, useCompatible: Boolean = prefersCompatible(track)): ByteArray {
        val path = if (useCompatible && track.hasCompatible) {
            "/api/tracks/${track.id}/download?version=compatible"
        } else {
            "/api/tracks/${track.id}/download"
        }
        return bodyOrError(client.get(url(path)) { auth() })
    }

    suspend fun metadataCandidates(trackId: String): MetadataCandidatesResponse =
        getJson("/api/tracks/$trackId/metadata/candidates")

    suspend fun requestMetadataLookup(trackId: String) {
        val response = client.post(url("/api/tracks/$trackId/metadata/lookup")) { auth() }
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
    }

    suspend fun metadataQueue(): MetadataQueueStatus = getJson("/api/metadata/queue")

    suspend fun applyMetadataCandidate(trackId: String, candidateId: String): Track =
        postJson("/api/tracks/$trackId/metadata/apply", ApplyMetadataRequest(candidateId))

    suspend fun playbackState(knownUpdatedAt: String? = null): PlaybackState? {
        val response = client.get(url("/api/playback")) {
            auth()
            if (!knownUpdatedAt.isNullOrBlank()) header(HttpHeaders.IfNoneMatch, "\"$knownUpdatedAt\"")
        }
        noteRefreshedToken(response)
        if (response.status == HttpStatusCode.NoContent || response.status == HttpStatusCode.NotModified) return null
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body()
    }

    suspend fun savePlayback(trackId: String, position: Double, queue: List<String>? = null): String {
        val response = client.put(url("/api/playback")) {
            auth()
            headers.append(HttpHeaders.ContentType, "application/json")
            setBody(PlaybackRequest(trackId, position, queue))
        }
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) throw ApiException(apiErrorMessage(response))
        return response.body<PlaybackSaved>().updatedAt
    }

    suspend fun favorites(): List<Track> = getJson("/api/favorites")

    suspend fun addFavorite(trackId: String) {
        expectNoContent(client.post(url("/api/favorites/$trackId")) { auth() })
    }

    suspend fun removeFavorite(trackId: String) {
        expectNoContent(client.delete(url("/api/favorites/$trackId")) { auth() })
    }

    suspend fun playlists(): List<Playlist> = getJson("/api/playlists")

    suspend fun createPlaylist(name: String): Playlist =
        postJson("/api/playlists", PlaylistRequest(name))

    suspend fun playlist(id: String): PlaylistDetail = getJson("/api/playlists/$id")

    suspend fun publicPlaylists(): List<Playlist> = getJson("/api/playlists/public")

    suspend fun setPlaylistPublic(id: String, isPublic: Boolean): Playlist =
        patchJson("/api/playlists/$id", UpdatePlaylistRequest(isPublic = isPublic))

    suspend fun pinPlaylist(id: String): Playlist =
        bodyOrError(client.post(url("/api/playlists/$id/pin")) { auth() })

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

    suspend fun search(query: String): SearchResults = getJson("/api/search") {
        parameter("q", query)
    }

    suspend fun stats(): Stats = getJson("/api/stats")

    suspend fun discoverSources(): List<DiscoverSource> = getJson("/api/discover/sources")

    suspend fun discoverSearch(source: String, query: String, page: Int = 1): DiscoverSearch =
        getJson("/api/discover/search") {
            parameter("source", source)
            parameter("q", query)
            parameter("page", page)
        }

    suspend fun discoverAdd(source: String, id: String): Track =
        postJson("/api/discover/add", DiscoverAddRequest(source, id))

    suspend fun trackShare(trackId: String): TrackShare? {
        val response = client.get(url("/api/tracks/$trackId/share")) { auth() }
        noteRefreshedToken(response)
        if (!response.status.isSuccess()) return null
        return response.body()
    }

    suspend fun createTrackShare(trackId: String): TrackShare =
        postJson("/api/tracks/$trackId/share", ShareRequest())

    suspend fun sharedTrack(token: String): Track = getJson("/api/share/$token/track")

    suspend fun deleteTrackShare(trackId: String) {
        expectNoContent(client.delete(url("/api/tracks/$trackId/share")) { auth() })
    }

    suspend fun libraryDuplicates(): List<DuplicateGroup> = getJson("/api/library/duplicates")

    suspend fun discoverImport(source: String, url: String): PlaylistImport =
        postJson("/api/discover/import", ImportRequest(source, url))

    suspend fun discoverImports(): List<PlaylistImport> = getJson("/api/discover/imports")

    suspend fun cancelImport(id: String) {
        expectNoContent(client.delete(url("/api/discover/imports/$id")) { auth() })
    }

    fun discoverArtworkUrl(source: String, coverUrl: String): String =
        url("/api/discover/artwork") + "?source=" + source.encodeURLParameter() +
            "&u=" + coverUrl.encodeURLParameter() +
            "&token=" + mediaOrSessionToken.encodeURLParameter()

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
