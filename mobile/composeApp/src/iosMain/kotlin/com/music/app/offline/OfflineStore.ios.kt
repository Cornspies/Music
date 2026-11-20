package com.music.app.offline

import kotlinx.cinterop.BetaInteropApi
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.usePinned
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.IO
import kotlinx.coroutines.withContext
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

    private val audio = FileCatalog(allNames(dir))

    private val extra = FileCatalog(allNames(extras))

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
        val name = "$trackId.$extension"
        val partialName = "$name$PART_SUFFIX"
        val target = "$dir/$name"
        val partial = "$dir/$partialName"
        audio.put(partialName, 0L)
        try {
            write(partial)
        } catch (e: Throwable) {
            fileManager.removeItemAtPath(partial, null)
            audio.remove(partialName)
            throw e
        }
        audio.names(trackId).filter { it != partialName }.forEach { remove(dir, audio, it) }
        audio.remove(partialName)
        val moved = fileManager.moveItemAtPath(partial, target, null)
        if (!moved) {
            fileManager.removeItemAtPath(partial, null)
            throw IllegalStateException("Could not save the download to device storage.")
        }
        audio.put(name, sizeOf(target))
        return target
    }

    actual fun pathFor(trackId: String): String? =
        audio.names(trackId).firstOrNull { !it.endsWith(PART_SUFFIX) }?.let { "$dir/$it" }

    actual fun delete(trackId: String) {
        audio.names(trackId).forEach { remove(dir, audio, it) }
        deleteExtras(trackId)
    }

    actual fun deleteAll() {
        allNames(dir).forEach { fileManager.removeItemAtPath("$dir/$it", null) }
        allNames(extras).forEach { fileManager.removeItemAtPath("$extras/$it", null) }
        audio.clear()
        extra.clear()
    }

    actual fun bytesUsed(): Long = audio.bytes + extra.bytes

    actual suspend fun measure() {
        val audioNames = audio.unmeasured()
        val extraNames = extra.unmeasured()
        val (audioSizes, extraSizes) = withContext(Dispatchers.IO) {
            sizesOf(dir, audioNames) to sizesOf(extras, extraNames)
        }
        audio.measured(audioSizes)
        extra.measured(extraSizes)
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
        val name = "$trackId.$kind"
        if (data.writeToFile("$extras/$name", atomically = true)) extra.put(name, bytes.size.toLong())
    }

    actual fun extraPath(trackId: String, kind: String): String? =
        "$trackId.$kind".takeIf { it in extra }?.let { "$extras/$it" }

    actual fun readExtra(trackId: String, kind: String): ByteArray? {
        val data = NSData.dataWithContentsOfFile("$extras/$trackId.$kind") ?: return null
        val out = ByteArray(data.length.toInt())
        if (out.isNotEmpty()) out.usePinned { memcpy(it.addressOf(0), data.bytes, data.length) }
        return out
    }

    actual fun deleteExtras(trackId: String) {
        extra.names(trackId).forEach { remove(extras, extra, it) }
    }

    actual fun saveMeta(key: String, value: String?) {
        if (value != null) defaults.setObject(value, forKey = key) else defaults.removeObjectForKey(key)
    }

    actual fun commitMeta(values: Map<String, String?>) {
        values.forEach { (key, value) -> saveMeta(key, value) }
        defaults.synchronize()
    }

    actual fun loadMeta(key: String): String? = defaults.stringForKey(key)

    private fun allNames(folder: String): List<String> =
        fileManager.contentsOfDirectoryAtPath(folder, null)
            ?.filterIsInstance<String>()
            ?: emptyList()

    private fun remove(folder: String, catalog: FileCatalog, name: String) {
        fileManager.removeItemAtPath("$folder/$name", null)
        catalog.remove(name)
    }

    private fun sizesOf(folder: String, names: List<String>): Map<String, Long> =
        names.associateWith { sizeOf("$folder/$it") }

    private fun sizeOf(path: String): Long =
        (fileManager.attributesOfItemAtPath(path, null)?.get(NSFileSize) as? NSNumber)?.longLongValue ?: 0L
}
