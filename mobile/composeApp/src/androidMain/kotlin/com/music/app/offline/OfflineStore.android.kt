package com.music.app.offline

import android.content.Context
import com.music.app.appContext
import java.io.File
import androidx.core.content.edit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

actual class OfflineStore actual constructor() {

    private val dir: File = File(appContext.filesDir, "offline").apply { mkdirs() }

    private val extras: File = File(appContext.filesDir, "offline-extras").apply { mkdirs() }

    private val prefs = appContext.getSharedPreferences("music-offline", Context.MODE_PRIVATE)

    private val audio = FileCatalog(dir.list().orEmpty().asList())

    private val extra = FileCatalog(extras.list().orEmpty().asList())

    actual suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String {
        if (!dir.exists()) dir.mkdirs()
        val target = File(dir, "$trackId.$extension")
        val partial = File(dir, "$trackId.$extension$PART_SUFFIX")
        audio.put(partial.name, 0L)
        try {
            write(partial.absolutePath)
        } catch (e: Throwable) {
            partial.delete()
            audio.remove(partial.name)
            throw e
        }
        audio.names(trackId).filter { it != partial.name }.forEach { remove(dir, audio, it) }
        audio.remove(partial.name)
        if (!partial.renameTo(target)) {
            partial.delete()
            throw IllegalStateException("Could not save the download to device storage.")
        }
        audio.put(target.name, target.length())
        return target.absolutePath
    }

    actual fun pathFor(trackId: String): String? =
        audio.names(trackId).firstOrNull { !it.endsWith(PART_SUFFIX) }?.let { File(dir, it).absolutePath }

    actual fun delete(trackId: String) {
        audio.names(trackId).forEach { remove(dir, audio, it) }
        deleteExtras(trackId)
    }

    actual fun deleteAll() {
        dir.listFiles()?.forEach { it.delete() }
        extras.listFiles()?.forEach { it.delete() }
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
        if (!extras.exists()) extras.mkdirs()
        val name = "$trackId.$kind"
        val partial = File(extras, "$name$PART_SUFFIX")
        partial.writeBytes(bytes)
        if (partial.renameTo(File(extras, name))) extra.put(name, bytes.size.toLong()) else partial.delete()
    }

    actual fun extraPath(trackId: String, kind: String): String? =
        "$trackId.$kind".takeIf { it in extra }?.let { File(extras, it).absolutePath }

    actual fun readExtra(trackId: String, kind: String): ByteArray? =
        File(extras, "$trackId.$kind").takeIf { it.isFile }?.readBytes()

    actual fun deleteExtras(trackId: String) {
        extra.names(trackId).forEach { remove(extras, extra, it) }
    }

    actual fun saveMeta(key: String, value: String?) {
        prefs.edit { putString(key, value) }
    }

    actual fun commitMeta(values: Map<String, String?>) {
        prefs.edit(commit = true) { values.forEach { (key, value) -> putString(key, value) } }
    }

    actual fun loadMeta(key: String): String? = prefs.getString(key, null)

    private fun remove(folder: File, catalog: FileCatalog, name: String) {
        File(folder, name).delete()
        catalog.remove(name)
    }

    private fun sizesOf(folder: File, names: List<String>): Map<String, Long> =
        names.associateWith { File(folder, it).length() }
}
