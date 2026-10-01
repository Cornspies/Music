package com.music.app.offline

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Foundation.NSApplicationSupportDirectory
import platform.Foundation.NSFileManager
import platform.Foundation.NSFileSize
import platform.Foundation.NSNumber
import platform.Foundation.NSURL
import platform.Foundation.NSURLIsExcludedFromBackupKey
import platform.Foundation.NSUserDefaults
import platform.Foundation.NSUserDomainMask

@OptIn(ExperimentalForeignApi::class)
actual class OfflineStore actual constructor() {

    private val fileManager = NSFileManager.defaultManager

    private val defaults = NSUserDefaults.standardUserDefaults

    private val dir: String = run {
        val base = fileManager.URLsForDirectory(NSApplicationSupportDirectory, NSUserDomainMask)
            .firstOrNull() as? NSURL
        val path = base?.path?.let { "$it/offline" } ?: "offline"
        if (!fileManager.fileExistsAtPath(path)) {
            fileManager.createDirectoryAtPath(path, withIntermediateDirectories = true, attributes = null, error = null)
            NSURL.fileURLWithPath(path).setResourceValue(NSNumber(bool = true), NSURLIsExcludedFromBackupKey, null)
        }
        path
    }

    actual suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String {
        if (!fileManager.fileExistsAtPath(dir)) {
            fileManager.createDirectoryAtPath(dir, withIntermediateDirectories = true, attributes = null, error = null)
        }
        val target = "$dir/$trackId.$extension"
        val partial = "$target$PART_SUFFIX"
        try {
            write(partial)
        } catch (e: Throwable) {
            fileManager.removeItemAtPath(partial, null)
            throw e
        }
        namesFor(trackId).map { "$dir/$it" }.filter { it != partial }.forEach { fileManager.removeItemAtPath(it, null) }
        val moved = fileManager.moveItemAtPath(partial, target, null)
        if (!moved) {
            fileManager.removeItemAtPath(partial, null)
            throw IllegalStateException("Could not save the download to device storage.")
        }
        return target
    }

    actual fun pathFor(trackId: String): String? =
        namesFor(trackId).firstOrNull { !it.endsWith(PART_SUFFIX) }?.let { "$dir/$it" }

    actual fun delete(trackId: String) {
        namesFor(trackId).forEach { fileManager.removeItemAtPath("$dir/$it", null) }
    }

    actual fun deleteAll() {
        allNames().forEach { fileManager.removeItemAtPath("$dir/$it", null) }
    }

    actual fun bytesUsed(): Long =
        allNames()
            .filter { !it.endsWith(PART_SUFFIX) }
            .sumOf { name ->
                val attrs = fileManager.attributesOfItemAtPath("$dir/$name", null)
                (attrs?.get(NSFileSize) as? NSNumber)?.longLongValue ?: 0L
            }

    actual fun saveMeta(key: String, value: String?) {
        if (value != null) defaults.setObject(value, forKey = key) else defaults.removeObjectForKey(key)
    }

    actual fun loadMeta(key: String): String? = defaults.stringForKey(key)

    private fun allNames(): List<String> =
        fileManager.contentsOfDirectoryAtPath(dir, null)
            ?.filterIsInstance<String>()
            ?: emptyList()

    private fun namesFor(trackId: String): List<String> =
        allNames().filter { it.startsWith("$trackId.") }

    private companion object {
        const val PART_SUFFIX = ".part"
    }
}
