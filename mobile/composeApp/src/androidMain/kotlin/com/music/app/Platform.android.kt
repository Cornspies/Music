package com.music.app

import android.content.Context
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

internal actual val canUploadFromDevice: Boolean = true

@Composable
internal actual fun rememberAudioPickerLauncher(
    onPicked: (List<PickedAudio>) -> Unit,
): () -> Unit {
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenMultipleDocuments(),
    ) { uris ->
        val picked = uris.map { uri ->
            val resolver = context.contentResolver
            var name = "upload.mp3"
            var size = 0L
            try {
                resolver.query(uri, null, null, null, null)?.use { cursor ->
                    if (cursor.moveToFirst()) {
                        val nameIdx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                        if (nameIdx >= 0) cursor.getString(nameIdx)?.let { name = it }
                        val sizeIdx = cursor.getColumnIndex(OpenableColumns.SIZE)
                        if (sizeIdx >= 0 && !cursor.isNull(sizeIdx)) size = cursor.getLong(sizeIdx)
                    }
                }
            } catch (_: Exception) {
            }
            val duration: Double? = queryDuration(context, uri)
            PickedAudio(name = name, sizeBytes = size, durationSec = duration, handle = uri)
        }
        if (picked.isNotEmpty()) onPicked(picked)
    }
    return { launcher.launch(arrayOf("audio/*", "application/ogg")) }
}

private fun queryDuration(context: Context, uri: Uri): Double? {
    val retriever = MediaMetadataRetriever()
    return try {
        retriever.setDataSource(context, uri)
        retriever
            .extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
            ?.toLongOrNull()
            ?.let { it / 1000.0 }
    } catch (_: Exception) {
        null
    } finally {
        try {
            retriever.release()
        } catch (_: Exception) {
        }
    }
}

internal actual suspend fun PickedAudio.readBytes(): ByteArray {
    val uri = handle as? Uri
        ?: throw IllegalStateException("missing file reference")
    val resolver = appContext.contentResolver
    return resolver.openInputStream(uri)?.use { it.readBytes() }
        ?: throw IllegalStateException("cannot read file")
}

internal actual val canPickImageFromDevice: Boolean = true

internal actual val unplayableMimeTypes: Set<String> = emptySet()

@Composable
internal actual fun rememberImagePickerLauncher(
    onPicked: (PickedImage) -> Unit,
): () -> Unit {
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument(),
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        val resolver = context.contentResolver
        var name = "avatar.jpg"
        try {
            resolver.query(uri, null, null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIdx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    if (nameIdx >= 0) cursor.getString(nameIdx)?.let { name = it }
                }
            }
        } catch (_: Exception) {
        }
        val mime = resolver.getType(uri) ?: "image/jpeg"
        onPicked(PickedImage(name = name, mimeType = mime, handle = uri))
    }
    return { launcher.launch(arrayOf("image/*")) }
}

internal actual suspend fun PickedImage.readBytes(): ByteArray {
    val uri = handle as? Uri
        ?: throw IllegalStateException("missing image reference")
    val resolver = appContext.contentResolver
    return resolver.openInputStream(uri)?.use { it.readBytes() }
        ?: throw IllegalStateException("cannot read image")
}
