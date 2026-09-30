package com.music.app.api

expect class TokenStore() {
    fun loadToken(): String?

    fun saveToken(token: String?)

    fun loadBaseUrl(): String

    fun saveBaseUrl(url: String)
}

internal expect val defaultBaseUrl: String

/**
 * The privacy policy, always the default server's: it covers the apps wherever
 * they connect, and another server's operator answers for their own server.
 */
internal val privacyPolicyUrl: String get() = "$defaultBaseUrl/privacy"
