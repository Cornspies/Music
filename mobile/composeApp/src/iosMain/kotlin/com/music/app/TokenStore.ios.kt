package com.music.app.api

import kotlinx.cinterop.BetaInteropApi
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.alloc
import kotlinx.cinterop.memScoped
import kotlinx.cinterop.ptr
import kotlinx.cinterop.usePinned
import kotlinx.cinterop.value
import platform.CoreFoundation.CFDictionaryAddValue
import platform.CoreFoundation.CFDictionaryCreateMutable
import platform.CoreFoundation.CFMutableDictionaryRef
import platform.CoreFoundation.CFTypeRef
import platform.CoreFoundation.CFTypeRefVar
import platform.CoreFoundation.kCFBooleanTrue
import platform.CoreFoundation.kCFTypeDictionaryKeyCallBacks
import platform.CoreFoundation.kCFTypeDictionaryValueCallBacks
import platform.Foundation.CFBridgingRelease
import platform.Foundation.CFBridgingRetain
import platform.Foundation.NSBundle
import platform.Foundation.NSData
import platform.Foundation.NSUserDefaults
import platform.Foundation.create
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
import platform.posix.memcpy

internal actual val defaultBaseUrl: String by lazy {
    val host = (NSBundle.mainBundle.objectForInfoDictionaryKey("MusicHost") as? String)?.trim().orEmpty()
    if (host.isEmpty() || host.contains("$(")) "" else "https://$host"
}

@OptIn(ExperimentalForeignApi::class)
actual class TokenStore {
    private val defaults = NSUserDefaults.standardUserDefaults

    actual fun loadToken(): String? = keychainRead()

    actual fun saveToken(token: String?) {
        keychainDelete()
        if (token != null) keychainWrite(token)
    }

    actual fun loadBaseUrl(): String =
        defaults.stringForKey("baseUrl") ?: defaultBaseUrl

    actual fun saveBaseUrl(url: String) {
        defaults.setObject(url, forKey = "baseUrl")
    }

    private fun baseQuery(): CFMutableDictionaryRef? {
        val dict = CFDictionaryCreateMutable(
            null, 0, kCFTypeDictionaryKeyCallBacks.ptr, kCFTypeDictionaryValueCallBacks.ptr,
        )
        CFDictionaryAddValue(dict, kSecClass, kSecClassGenericPassword)
        CFDictionaryAddValue(dict, kSecAttrService, retained(SERVICE))
        CFDictionaryAddValue(dict, kSecAttrAccount, retained(ACCOUNT))
        return dict
    }

    private fun retained(s: String): CFTypeRef? = CFBridgingRetain(s)

    private fun keychainWrite(token: String) {
        val data = token.encodeToByteArray().toNSData()
        val query = baseQuery()
        CFDictionaryAddValue(query, kSecValueData, CFBridgingRetain(data))
        CFDictionaryAddValue(query, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)
        val status = SecItemAdd(query, null)
        if (status != errSecSuccess) {
            keychainDelete()
            SecItemAdd(query, null)
        }
        CFBridgingRelease(query)
    }

    private fun keychainRead(): String? {
        val query = baseQuery()
        CFDictionaryAddValue(query, kSecReturnData, kCFBooleanTrue)
        CFDictionaryAddValue(query, kSecMatchLimit, kSecMatchLimitOne)
        return memScoped {
            val result = alloc<CFTypeRefVar>()
            val status = SecItemCopyMatching(query, result.ptr)
            CFBridgingRelease(query)
            if (status == errSecItemNotFound || status != errSecSuccess) return@memScoped null
            val data = CFBridgingRelease(result.value) as? NSData ?: return@memScoped null
            data.toByteArray().decodeToString()
        }
    }

    private fun keychainDelete() {
        val query = baseQuery()
        SecItemDelete(query)
        CFBridgingRelease(query)
    }

    private companion object {
        const val SERVICE = "com.music.app.session"
        const val ACCOUNT = "jwt"
    }
}

@OptIn(ExperimentalForeignApi::class, BetaInteropApi::class)
private fun ByteArray.toNSData(): NSData =
    if (isEmpty()) NSData() else usePinned { NSData.create(bytes = it.addressOf(0), length = size.toULong()) }

@OptIn(ExperimentalForeignApi::class)
private fun NSData.toByteArray(): ByteArray {
    val size = length.toInt()
    if (size == 0) return ByteArray(0)
    return ByteArray(size).also { out -> out.usePinned { memcpy(it.addressOf(0), bytes, length) } }
}
