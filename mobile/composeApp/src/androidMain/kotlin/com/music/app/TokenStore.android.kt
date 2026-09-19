package com.music.app.api

import android.content.Context
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

internal actual val defaultBaseUrl: String = "http://10.0.2.2:3001" // emulator → host loopback

/**
 * Token storage backed by the Android Keystore.
 *
 * The JWT is encrypted with an AES-GCM key that lives in hardware-backed
 * storage (the key itself never leaves the Keystore), and only the ciphertext
 * goes into SharedPreferences. A copy of the prefs file — a backup, a rooted
 * device's file browser, a stray `adb backup` — is therefore useless without
 * this device's key. Backups are switched off in the manifest as well, so the
 * ciphertext doesn't travel either.
 *
 * The server URL is not a secret and stays in plain prefs.
 */
actual class TokenStore {
    private val prefs = appContext.getSharedPreferences("music", Context.MODE_PRIVATE)

    actual fun loadToken(): String? {
        migrateLegacyToken()
        val stored = prefs.getString(KEY_TOKEN_ENCRYPTED, null) ?: return null
        return runCatching { decrypt(stored) }.getOrElse {
            // Key gone (factory reset restore, keystore wiped): the token
            // can't be read and the user signs in again.
            prefs.edit { remove(KEY_TOKEN_ENCRYPTED) }
            null
        }
    }

    actual fun saveToken(token: String?) {
        prefs.edit {
            remove(KEY_TOKEN_LEGACY)
            if (token == null) {
                remove(KEY_TOKEN_ENCRYPTED)
            } else {
                putString(KEY_TOKEN_ENCRYPTED, encrypt(token))
            }
        }
    }

    actual fun loadBaseUrl(): String = prefs.getString("baseUrl", null) ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        prefs.edit { putString("baseUrl", url) }
    }

    /** Older builds stored the token in the clear; move it across once. */
    private fun migrateLegacyToken() {
        val legacy = prefs.getString(KEY_TOKEN_LEGACY, null) ?: return
        runCatching { saveToken(legacy) }
        prefs.edit { remove(KEY_TOKEN_LEGACY) }
    }

    // ---- crypto -------------------------------------------------------------

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
        // iv length prefix + iv + ciphertext, so a future IV size can't be
        // confused with today's.
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
        const val KEY_TOKEN_LEGACY = "token"
    }
}
