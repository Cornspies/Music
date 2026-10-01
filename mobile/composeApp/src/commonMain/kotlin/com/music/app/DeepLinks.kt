package com.music.app

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

object DeepLinks {

    var pendingPlaylistId by mutableStateOf<String?>(null)
        private set

    fun open(playlistId: String) {
        pendingPlaylistId = playlistId
    }

    var pendingInviteCode by mutableStateOf<String?>(null)
        private set

    fun consumeInvite(): String? {
        val code = pendingInviteCode
        pendingInviteCode = null
        return code
    }

    var pendingShareToken by mutableStateOf<String?>(null)
        private set

    var pendingListenCode by mutableStateOf<String?>(null)
        private set

    fun consumeListenCode(): String? {
        val code = pendingListenCode
        pendingListenCode = null
        return code
    }

    fun consumeShareToken(): String? {
        val token = pendingShareToken
        pendingShareToken = null
        return token
    }

    fun handle(url: String?) {
        inviteCodeFrom(url)?.let { pendingInviteCode = it }
        playlistIdFrom(url)?.let { pendingPlaylistId = it }
        shareTokenFrom(url)?.let { pendingShareToken = it }
        listenCodeFrom(url)?.let { pendingListenCode = it }
    }

    fun listenCodeFrom(url: String?): String? {
        val raw = url?.trim().orEmpty()
        val path = raw.substringAfter("://", raw).substringBefore('?').substringBefore('#')
        val segments = path.split('/').filter { it.isNotBlank() }
        val marker = segments.indexOf("listen")
        if (marker < 1 || marker != segments.lastIndex - 1) return null
        return segments[marker + 1].takeIf { code -> code.all { it.isLetterOrDigit() || it == '-' || it == '_' } }
    }

    fun listenUrl(baseUrl: String, code: String): String {
        val root = baseUrl.trim().trimEnd('/')
        return "$root/listen/$code"
    }

    fun shareTokenFrom(url: String?): String? {
        val raw = url?.trim().orEmpty()
        val path = raw.substringAfter("://", raw).substringBefore('?').substringBefore('#')
        val segments = path.split('/').filter { it.isNotBlank() }
        val marker = segments.indexOf("s")
        if (marker < 1 || marker != segments.lastIndex - 1) return null
        return segments[marker + 1].takeIf { token -> token.all { it.isLetterOrDigit() || it == '-' || it == '_' } }
    }

    fun inviteCodeFrom(url: String?): String? {
        val raw = url?.trim().orEmpty()
        if (!raw.contains("/register")) return null
        val query = raw.substringAfter('?', "").substringBefore('#')
        return query.split('&')
            .map { it.substringBefore('=') to it.substringAfter('=', "") }
            .firstOrNull { it.first == "invite" }
            ?.second
            ?.takeIf { it.isNotBlank() }
    }

    fun consume(): String? {
        val id = pendingPlaylistId
        pendingPlaylistId = null
        return id
    }

    fun playlistIdFrom(url: String?): String? {
        val raw = url?.trim().orEmpty()
        if (raw.isEmpty()) return null

        val afterScheme = raw.substringAfter("://", raw)
        val path = afterScheme.substringBefore('?').substringBefore('#')

        val segments = path.split('/').filter { it.isNotBlank() }
        if (segments.isEmpty()) return null

        val marker = segments.indexOfLast { it == "playlists" || it == "playlist" }
        if (marker < 0 || marker == segments.lastIndex) return null

        val id = segments[marker + 1]
        return id.takeIf { it.isNotBlank() && it.none { c -> c == ':' } }
    }

    fun shareUrl(baseUrl: String, playlistId: String): String {
        val root = baseUrl.trim().trimEnd('/')
        return "$root/playlists/$playlistId"
    }

    fun trackShareUrl(baseUrl: String, token: String): String {
        val root = baseUrl.trim().trimEnd('/')
        return "$root/s/$token"
    }
}
