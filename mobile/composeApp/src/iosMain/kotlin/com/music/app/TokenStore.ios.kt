package com.music.app.api

import platform.Foundation.NSUserDefaults

internal actual val defaultBaseUrl: String = "http://localhost:3001" // simulator → host loopback

/** NSUserDefaults-backed storage for the JWT + server base URL. */
actual class TokenStore {
    private val defaults = NSUserDefaults.standardUserDefaults

    actual fun loadToken(): String? = defaults.stringForKey("token")

    actual fun saveToken(token: String?) {
        if (token != null) {
            defaults.setObject(token, forKey = "token")
        } else {
            defaults.removeObjectForKey("token")
        }
    }

    actual fun loadBaseUrl(): String =
        defaults.stringForKey("baseUrl") ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        defaults.setObject(url, forKey = "baseUrl")
    }
}
