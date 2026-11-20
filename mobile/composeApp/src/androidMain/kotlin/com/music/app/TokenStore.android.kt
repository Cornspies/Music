package com.music.app.api

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.core.content.edit
import com.music.app.appContext
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal actual val defaultBaseUrl: String by lazy {
    runCatching {
        val pm = appContext.packageManager
        val info = if (Build.VERSION.SDK_INT >= 33) {
            val flags = PackageManager.ApplicationInfoFlags.of(PackageManager.GET_META_DATA.toLong())
            pm.getApplicationInfo(appContext.packageName, flags)
        } else {
            @Suppress("DEPRECATION")
            pm.getApplicationInfo(appContext.packageName, PackageManager.GET_META_DATA)
        }
        info.metaData?.getString("com.music.app.DEFAULT_SERVER").orEmpty().trim()
    }.getOrDefault("")
}

actual class TokenStore {
    private val prefs = appContext.getSharedPreferences("music", Context.MODE_PRIVATE)

    // The token is read for every request, and decrypting it asks the system
    // keystore each time, so it is kept after the first read.
    @Volatile private var cached: String? = null
    @Volatile private var loaded = false

    actual fun loadToken(): String? {
        if (loaded) return cached
        val token = prefs.getString(KEY_TOKEN_ENCRYPTED, null)?.let { stored ->
            runCatching { decrypt(stored) }.getOrElse {
                prefs.edit { remove(KEY_TOKEN_ENCRYPTED) }
                null
            }
        }
        cached = token
        loaded = true
        return token
    }

    actual fun saveToken(token: String?) {
        prefs.edit {
            if (token == null) {
                remove(KEY_TOKEN_ENCRYPTED)
            } else {
                putString(KEY_TOKEN_ENCRYPTED, encrypt(token))
            }
        }
        cached = token
        loaded = true
    }

    actual fun loadBaseUrl(): String = prefs.getString("baseUrl", null) ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        prefs.edit { putString("baseUrl", url) }
    }

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (ks.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private fun encrypt(plain: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val iv = cipher.iv
        val body = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
        val out = ByteArray(1 + iv.size + body.size)
        out[0] = iv.size.toByte()
        iv.copyInto(out, 1)
        body.copyInto(out, 1 + iv.size)
        return Base64.encodeToString(out, Base64.NO_WRAP)
    }

    private fun decrypt(encoded: String): String {
        val bytes = Base64.decode(encoded, Base64.NO_WRAP)
        val ivLen = bytes[0].toInt()
        val iv = bytes.copyOfRange(1, 1 + ivLen)
        val body = bytes.copyOfRange(1 + ivLen, bytes.size)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, iv))
        return String(cipher.doFinal(body), Charsets.UTF_8)
    }

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val KEY_ALIAS = "music.session"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val KEY_TOKEN_ENCRYPTED = "token.enc"
    }
}
