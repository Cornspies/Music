package com.music.app

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * Incoming share links.
 *
 * A shared playlist is just the web app's own URL — `https://host/playlists/<id>`
 * — so a link works in a browser whether or not the app is installed. On
 * Android the manifest claims that path for the configured host, and
 * [MainActivity] hands the URL here.
 *
 * The link is held rather than acted on immediately, because it usually
 * arrives before the app has finished signing in; [App] consumes it once
 * there's a session.
 */
object DeepLinks {

    /** Playlist waiting to be opened, or null. */
    var pendingPlaylistId by mutableStateOf<String?>(null)
        private set

    /** Records an incoming link; [App] opens it once there's a session. */
    fun open(playlistId: String) {
        pendingPlaylistId = playlistId
    }

    /**
     * Invite code from a `/register?invite=CODE` link, waiting for the sign-in
     * screen to pick it up. Cleared once used.
     */
    var pendingInviteCode by mutableStateOf<String?>(null)
        private set

    fun consumeInvite(): String? {
        val code = pendingInviteCode
        pendingInviteCode = null
        return code
    }

    /** Routes any incoming URL to the right pending slot. */
    fun handle(url: String?) {
        inviteCodeFrom(url)?.let { pendingInviteCode = it }
        playlistIdFrom(url)?.let { pendingPlaylistId = it }
    }

    /** The invite code in a `/register?invite=CODE` link, or null. */
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

    /** Takes the pending playlist id, clearing it so it's opened only once. */
    fun consume(): String? {
        val id = pendingPlaylistId
        pendingPlaylistId = null
        return id
    }

    /**
     * Extracts the playlist id from a share link.
     *
     * Accepts the web URL in any of its usual shapes — with or without a
     * scheme, with a port, with a query string or fragment, with a trailing
     * slash — plus the `music://playlist/<id>` custom scheme, which is what
     * gets used when the app is installed but the host isn't configured for
     * app links. Returns null for anything that isn't a playlist link.
     */
    fun playlistIdFrom(url: String?): String? {
        val raw = url?.trim().orEmpty()
        if (raw.isEmpty()) return null

        // Strip scheme, then query/fragment.
        val afterScheme = raw.substringAfter("://", raw)
        val path = afterScheme.substringBefore('?').substringBefore('#')

        val segments = path.split('/').filter { it.isNotBlank() }
        if (segments.isEmpty()) return null

        // music://playlist/<id> — "playlist" is the authority, so after
        // stripping the scheme the segments are ["playlist", "<id>"].
        // Web links are <host>/playlists/<id>, possibly under a sub-path.
        val marker = segments.indexOfLast { it == "playlists" || it == "playlist" }
        if (marker < 0 || marker == segments.lastIndex) return null

        val id = segments[marker + 1]
        return id.takeIf { it.isNotBlank() && it.none { c -> c == ':' } }
    }

    /**
     * Builds the shareable URL for a playlist from the configured server
     * address. The web app and the API share an origin in the normal setup, so
     * the API base URL is the site root.
     */
    fun shareUrl(baseUrl: String, playlistId: String): String {
        val root = baseUrl.trim().trimEnd('/')
        return "$root/playlists/$playlistId"
    }
}
