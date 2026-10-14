package com.music.app.api

expect class TokenStore() {
    fun loadToken(): String?

    fun saveToken(token: String?)

    fun loadBaseUrl(): String

    fun saveBaseUrl(url: String)
}

internal expect val defaultBaseUrl: String

internal fun privacyPolicyUrl(serverUrl: String): String? {
    val base = serverUrl.trim().trimEnd('/')
    if (base.isEmpty()) return null
    return (if ("://" in base) base else "https://$base") + "/privacy"
}
