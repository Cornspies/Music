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

    /**
     * The server a pending link came from, e.g. https://music.example.com, or
     * null for links that don't name one. Links reach the app either as the
     * server's own https address (verified app links, universal links) or
     * wrapped as cornspies-music://open?url=<address>, which the web app's
     * "Open in the app" button uses for servers the app can't be verified for.
     */
    var linkServer by mutableStateOf<String?>(null)
        private set

    fun handle(url: String?) {
        val target = unwrap(url) ?: return
        val invite = inviteCodeFrom(target)
        val playlist = playlistIdFrom(target)
        val share = shareTokenFrom(target)
        val listen = listenCodeFrom(target)
        if (invite == null && playlist == null && share == null && listen == null) return
        linkServer = serverOf(target)
        invite?.let { pendingInviteCode = it }
        playlist?.let { pendingPlaylistId = it }
        share?.let { pendingShareToken = it }
        listen?.let { pendingListenCode = it }
    }

    /**
     * Drops the pending links when they belong to a server other than
     * [baseUrl], and returns that server's host so the caller can say so.
     */
    fun rejectForeign(baseUrl: String): String? {
        val server = linkServer ?: return null
        if (normalized(server) == normalized(baseUrl)) return null
        pendingPlaylistId = null
        pendingShareToken = null
        pendingListenCode = null
        pendingInviteCode = null
        linkServer = null
        return server.substringAfter("://").substringBefore('/')
    }

    private fun unwrap(url: String?): String? {
        val raw = url?.trim().orEmpty()
        if (!raw.startsWith("$APP_SCHEME://", ignoreCase = true)) return raw.ifEmpty { null }
        val encoded = raw.substringAfter('?', "").substringBefore('#').split('&')
            .firstOrNull { it.substringBefore('=') == "url" }
            ?.substringAfter('=', "")
            ?: return null
        val target = percentDecode(encoded)
        return target.takeIf { it.startsWith("https://", true) || it.startsWith("http://", true) }
    }

    /** The server's address: the link without the app route at its end, so a server under a path keeps it. */
    private fun serverOf(url: String): String? {
        if (!url.startsWith("https://", true) && !url.startsWith("http://", true)) return null
        val scheme = url.substringBefore("://")
        val rest = url.substringAfter("://").substringBefore('?').substringBefore('#')
        val host = rest.substringBefore('/')
        if (host.isEmpty()) return null
        val segments = rest.substringAfter('/', "").split('/').filter { it.isNotEmpty() }
        val route = segments.indexOfLast { it in ROUTE_ROOTS }
        val base = if (route >= 0) segments.take(route) else segments
        return (listOf("$scheme://$host") + base).joinToString("/")
    }

    private fun normalized(server: String): String {
        val trimmed = server.trim().trimEnd('/')
        val scheme = trimmed.substringBefore("://", "").lowercase()
        val rest = trimmed.substringAfter("://")
        var host = rest.substringBefore('/').lowercase()
        if (scheme == "https") host = host.removeSuffix(":443") else if (scheme == "http") host = host.removeSuffix(":80")
        val path = rest.substringAfter('/', "")
        return if (path.isEmpty()) "$scheme://$host" else "$scheme://$host/$path"
    }

    private fun percentDecode(s: String): String {
        val bytes = ArrayList<Byte>(s.length)
        var i = 0
        while (i < s.length) {
            val c = s[i]
            if (c == '%' && i + 2 <= s.lastIndex) {
                val hex = s.substring(i + 1, i + 3).toIntOrNull(16)
                if (hex != null) {
                    bytes.add(hex.toByte())
                    i += 3
                    continue
                }
            }
            if (c == '+') bytes.add(' '.code.toByte()) else c.toString().encodeToByteArray().forEach { bytes.add(it) }
            i++
        }
        return bytes.toByteArray().decodeToString()
    }

    private const val APP_SCHEME = "cornspies-music"
    private val ROUTE_ROOTS = setOf("playlists", "s", "listen", "register")

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

        val marker = segments.indexOfLast { it == "playlists" }
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
