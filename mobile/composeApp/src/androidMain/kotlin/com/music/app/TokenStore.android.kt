package com.music.app.api

import android.content.Context
import com.music.app.appContext
import androidx.core.content.edit

internal actual val defaultBaseUrl: String = "http://10.0.2.2:3001" // emulator → host loopback

/** SharedPreferences-backed storage for the JWT + server base URL. */
actual class TokenStore {
    private val prefs = appContext.getSharedPreferences("music", Context.MODE_PRIVATE)

    actual fun loadToken(): String? = prefs.getString("token", null)

    actual fun saveToken(token: String?) {
        prefs.edit {putString("token", token)}
    }

    actual fun loadBaseUrl(): String = prefs.getString("baseUrl", null) ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        prefs.edit { putString("baseUrl", url) }
    }
}
