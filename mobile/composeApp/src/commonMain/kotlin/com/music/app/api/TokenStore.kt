package com.music.app.api

expect class TokenStore() {
    fun loadToken(): String?

    fun saveToken(token: String?)

    fun loadBaseUrl(): String

    fun saveBaseUrl(url: String)
}

internal expect val defaultBaseUrl: String
