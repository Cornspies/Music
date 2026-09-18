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
 * Platform default for the server URL:
 *  - Android: http://10.0.2.2:3001 (emulator → host loopback).
 *  - iOS:     http://localhost:3001 (simulator → host loopback).
 * Real devices must set the machine's LAN IP on the login screen.
 */
internal expect val defaultBaseUrl: String
