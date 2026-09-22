package com.music.app.api

/**
 * Platform-backed key/value storage for the JWT and the server base URL.
 *  - Android: SharedPreferences ("music" prefs file).
 *  - iOS:     NSUserDefaults (standard user defaults).
 */
expect class TokenStore() {
    /** The stored JWT, or null when logged out / never logged in. */
    fun loadToken(): String?

    fun saveToken(token: String?)

    /** The persisted server base URL (no trailing slash), or the platform default. */
    fun loadBaseUrl(): String

    fun saveBaseUrl(url: String)
}

/**
 * The server a fresh installation talks to, until someone types another one
 * on the login screen. It is the site's origin, not its API path: every
 * request appends "/api/…" itself, and share links are built from the same
 * value. What is typed is taken as it stands — a server may well live under
 * a path, and quietly rewriting it would only hide the mistake.
 *
 * Both platforms default to the public instance; point them at your own
 * server by changing this (and `musicHost` in androidApp/build.gradle.kts,
 * which is what makes its links open the app). For development against a
 * server on your machine, type http://10.0.2.2:3001 on the login screen from
 * the Android emulator, http://localhost:3001 from the iOS simulator, or the
 * machine's LAN address from a real phone.
 */
internal expect val defaultBaseUrl: String
