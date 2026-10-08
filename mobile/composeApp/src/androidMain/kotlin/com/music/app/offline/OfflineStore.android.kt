package com.music.app.offline

import android.content.Context
import com.music.app.appContext
import java.io.File
import androidx.core.content.edit

actual class OfflineStore actual constructor() {

    private val dir: File = File(appContext.filesDir, "offline").apply { mkdirs() }

    private val extras: File = File(appContext.filesDir, "offline-extras").apply { mkdirs() }

    private val prefs = appContext.getSharedPreferences("music-offline", Context.MODE_PRIVATE)

    actual suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String {
        if (!dir.exists()) dir.mkdirs()
        val target = File(dir, "$trackId.$extension")
        val partial = File(dir, "$trackId.$extension$PART_SUFFIX")
        try {
            write(partial.absolutePath)
        } catch (e: Throwable) {
            partial.delete()
            throw e
        }
        filesFor(trackId).filter { it != partial }.forEach { it.delete() }
        if (!partial.renameTo(target)) {
            partial.delete()
            throw IllegalStateException("Could not save the download to device storage.")
        }
        return target.absolutePath
    }

    actual fun pathFor(trackId: String): String? =
        filesFor(trackId).firstOrNull { !it.name.endsWith(PART_SUFFIX) }?.absolutePath

    actual fun delete(trackId: String) {
        filesFor(trackId).forEach { it.delete() }
        deleteExtras(trackId)
    }

    actual fun deleteAll() {
        dir.listFiles()?.forEach { it.delete() }
        extras.listFiles()?.forEach { it.delete() }
    }

    actual fun bytesUsed(): Long =
        listOf(dir, extras).sumOf { folder ->
            folder.listFiles()
                ?.filter { it.isFile && !it.name.endsWith(PART_SUFFIX) }
                ?.sumOf { it.length() }
                ?: 0L
        }

    actual fun saveExtra(trackId: String, kind: String, bytes: ByteArray) {
        if (!extras.exists()) extras.mkdirs()
        val partial = File(extras, "$trackId.$kind$PART_SUFFIX")
        partial.writeBytes(bytes)
        if (!partial.renameTo(File(extras, "$trackId.$kind"))) partial.delete()
    }

    actual fun extraPath(trackId: String, kind: String): String? =
        File(extras, "$trackId.$kind").takeIf { it.isFile }?.absolutePath

    actual fun readExtra(trackId: String, kind: String): ByteArray? =
        File(extras, "$trackId.$kind").takeIf { it.isFile }?.readBytes()

    actual fun deleteExtras(trackId: String) {
        extras.listFiles()?.filter { it.name.startsWith("$trackId.") }?.forEach { it.delete() }
    }

    actual fun saveMeta(key: String, value: String?) {
        prefs.edit { putString(key, value) }
    }

    actual fun loadMeta(key: String): String? = prefs.getString(key, null)

    private fun filesFor(trackId: String): List<File> =
        dir.listFiles()?.filter { it.isFile && it.name.startsWith("$trackId.") } ?: emptyList()

    private companion object {
        const val PART_SUFFIX = ".part"
    }
}
