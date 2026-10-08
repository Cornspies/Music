package com.music.app.offline

import kotlinx.cinterop.BetaInteropApi
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.usePinned
import platform.Foundation.NSApplicationSupportDirectory
import platform.Foundation.NSData
import platform.Foundation.NSFileManager
import platform.Foundation.NSFileSize
import platform.Foundation.NSNumber
import platform.Foundation.NSURL
import platform.Foundation.NSURLIsExcludedFromBackupKey
import platform.Foundation.NSUserDefaults
import platform.Foundation.NSUserDomainMask
import platform.Foundation.create
import platform.Foundation.dataWithContentsOfFile
import platform.Foundation.writeToFile
import platform.posix.memcpy

@OptIn(ExperimentalForeignApi::class, BetaInteropApi::class)
actual class OfflineStore actual constructor() {

    private val fileManager = NSFileManager.defaultManager

    private val defaults = NSUserDefaults.standardUserDefaults

    private val dir: String = folder("offline")

    private val extras: String = folder("offline-extras")

    private fun folder(name: String): String {
        val base = fileManager.URLsForDirectory(NSApplicationSupportDirectory, NSUserDomainMask)
            .firstOrNull() as? NSURL
        val path = base?.path?.let { "$it/$name" } ?: name
        if (!fileManager.fileExistsAtPath(path)) {
            fileManager.createDirectoryAtPath(path, withIntermediateDirectories = true, attributes = null, error = null)
            NSURL.fileURLWithPath(path).setResourceValue(NSNumber(bool = true), NSURLIsExcludedFromBackupKey, null)
        }
        return path
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
        deleteExtras(trackId)
    }

    actual fun deleteAll() {
        allNames().forEach { fileManager.removeItemAtPath("$dir/$it", null) }
        allNames(extras).forEach { fileManager.removeItemAtPath("$extras/$it", null) }
    }

    actual fun bytesUsed(): Long =
        listOf(dir, extras).sumOf { folder ->
            allNames(folder)
                .filter { !it.endsWith(PART_SUFFIX) }
                .sumOf { name ->
                    val attrs = fileManager.attributesOfItemAtPath("$folder/$name", null)
                    (attrs?.get(NSFileSize) as? NSNumber)?.longLongValue ?: 0L
                }
        }

    actual fun saveExtra(trackId: String, kind: String, bytes: ByteArray) {
        if (!fileManager.fileExistsAtPath(extras)) {
            fileManager.createDirectoryAtPath(extras, withIntermediateDirectories = true, attributes = null, error = null)
        }
        val data = if (bytes.isEmpty()) {
            NSData()
        } else {
            bytes.usePinned { NSData.create(bytes = it.addressOf(0), length = bytes.size.toULong()) }
        }
        data.writeToFile("$extras/$trackId.$kind", atomically = true)
    }

    actual fun extraPath(trackId: String, kind: String): String? =
        "$extras/$trackId.$kind".takeIf { fileManager.fileExistsAtPath(it) }

    actual fun readExtra(trackId: String, kind: String): ByteArray? {
        val data = NSData.dataWithContentsOfFile("$extras/$trackId.$kind") ?: return null
        val out = ByteArray(data.length.toInt())
        if (out.isNotEmpty()) out.usePinned { memcpy(it.addressOf(0), data.bytes, data.length) }
        return out
    }

    actual fun deleteExtras(trackId: String) {
        allNames(extras).filter { it.startsWith("$trackId.") }.forEach { fileManager.removeItemAtPath("$extras/$it", null) }
    }

    actual fun saveMeta(key: String, value: String?) {
        if (value != null) defaults.setObject(value, forKey = key) else defaults.removeObjectForKey(key)
    }

    actual fun loadMeta(key: String): String? = defaults.stringForKey(key)

    private fun allNames(folder: String = dir): List<String> =
        fileManager.contentsOfDirectoryAtPath(folder, null)
            ?.filterIsInstance<String>()
            ?: emptyList()

    private fun namesFor(trackId: String): List<String> =
        allNames().filter { it.startsWith("$trackId.") }

    private companion object {
        const val PART_SUFFIX = ".part"
    }
}
