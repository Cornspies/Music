package com.music.app.api

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.alloc
import kotlinx.cinterop.memScoped
import kotlinx.cinterop.ptr
import kotlinx.cinterop.value
import platform.CoreFoundation.CFDictionaryAddValue
import platform.CoreFoundation.CFDictionaryCreateMutable
import platform.CoreFoundation.CFDictionaryRef
import platform.CoreFoundation.CFMutableDictionaryRef
import platform.CoreFoundation.CFTypeRef
import platform.CoreFoundation.CFTypeRefVar
import platform.CoreFoundation.kCFBooleanTrue
import platform.CoreFoundation.kCFTypeDictionaryKeyCallBacks
import platform.CoreFoundation.kCFTypeDictionaryValueCallBacks
import platform.Foundation.CFBridgingRelease
import platform.Foundation.CFBridgingRetain
import platform.Foundation.NSData
import platform.Foundation.NSString
import platform.Foundation.NSUTF8StringEncoding
import platform.Foundation.NSUserDefaults
import platform.Foundation.create
import platform.Foundation.dataUsingEncoding
import platform.Security.SecItemAdd
import platform.Security.SecItemCopyMatching
import platform.Security.SecItemDelete
import platform.Security.errSecItemNotFound
import platform.Security.errSecSuccess
import platform.Security.kSecAttrAccessible
import platform.Security.kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
import platform.Security.kSecAttrAccount
import platform.Security.kSecAttrService
import platform.Security.kSecClass
import platform.Security.kSecClassGenericPassword
import platform.Security.kSecMatchLimit
import platform.Security.kSecMatchLimitOne
import platform.Security.kSecReturnData
import platform.Security.kSecValueData

internal actual val defaultBaseUrl: String = "http://localhost:3001" // simulator → host loopback

/**
 * Token storage backed by the iOS Keychain.
 *
 * The JWT is a generic-password item marked AfterFirstUnlockThisDeviceOnly:
 * readable once the phone has been unlocked since boot (so background audio
 * keeps its session), never included in backups, never migrated to another
 * device. The server URL is not a secret and stays in NSUserDefaults.
 */
@OptIn(ExperimentalForeignApi::class)
actual class TokenStore {
    private val defaults = NSUserDefaults.standardUserDefaults

    actual fun loadToken(): String? {
        migrateLegacyToken()
        return keychainRead()
    }

    actual fun saveToken(token: String?) {
        defaults.removeObjectForKey(LEGACY_KEY)
        keychainDelete()
        if (token != null) keychainWrite(token)
    }

    actual fun loadBaseUrl(): String =
        defaults.stringForKey("baseUrl") ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        defaults.setObject(url, forKey = "baseUrl")
    }

    /** Older builds kept the token in user defaults; move it across once. */
    private fun migrateLegacyToken() {
        val legacy = defaults.stringForKey(LEGACY_KEY) ?: return
        if (keychainRead() == null) keychainWrite(legacy)
        defaults.removeObjectForKey(LEGACY_KEY)
    }

    // ---- keychain -----------------------------------------------------------

    private fun baseQuery(): CFMutableDictionaryRef? {
        val dict = CFDictionaryCreateMutable(
            null, 0, kCFTypeDictionaryKeyCallBacks.ptr, kCFTypeDictionaryValueCallBacks.ptr,
        )
        CFDictionaryAddValue(dict, kSecClass, kSecClassGenericPassword)
        CFDictionaryAddValue(dict, kSecAttrService, retained(SERVICE))
        CFDictionaryAddValue(dict, kSecAttrAccount, retained(ACCOUNT))
        return dict
    }

    private fun retained(s: String): CFTypeRef? = CFBridgingRetain(NSString.create(string = s))

    private fun keychainWrite(token: String) {
        val data = (NSString.create(string = token) as NSString).dataUsingEncoding(NSUTF8StringEncoding) ?: return
        val query = baseQuery()
        CFDictionaryAddValue(query, kSecValueData, CFBridgingRetain(data))
        CFDictionaryAddValue(query, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)
        val status = SecItemAdd(query as CFDictionaryRef?, null)
        if (status != errSecSuccess) {
            // Most likely a stale duplicate; clear and retry once.
            keychainDelete()
            SecItemAdd(query as CFDictionaryRef?, null)
        }
        CFBridgingRelease(query)
    }

    private fun keychainRead(): String? {
        val query = baseQuery()
        CFDictionaryAddValue(query, kSecReturnData, kCFBooleanTrue)
        CFDictionaryAddValue(query, kSecMatchLimit, kSecMatchLimitOne)
        return memScoped {
            val result = alloc<CFTypeRefVar>()
            val status = SecItemCopyMatching(query as CFDictionaryRef?, result.ptr)
            CFBridgingRelease(query)
            if (status == errSecItemNotFound || status != errSecSuccess) return@memScoped null
            val data = CFBridgingRelease(result.value) as? NSData ?: return@memScoped null
            NSString.create(data = data, encoding = NSUTF8StringEncoding) as String?
        }
    }

    private fun keychainDelete() {
        val query = baseQuery()
        SecItemDelete(query as CFDictionaryRef?)
        CFBridgingRelease(query)
    }

    private companion object {
        const val SERVICE = "com.music.app.session"
        const val ACCOUNT = "jwt"
        const val LEGACY_KEY = "token"
    }
}
