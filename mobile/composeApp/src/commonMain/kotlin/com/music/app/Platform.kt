package com.music.app

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.decodeToImageBitmap

internal expect val canUploadFromDevice: Boolean

class PickedAudio(
    val name: String,
    val sizeBytes: Long,
    val durationSec: Double?,
    val handle: Any?,
)

@Composable
internal expect fun rememberAudioPickerLauncher(onPicked: (List<PickedAudio>) -> Unit): () -> Unit

internal expect suspend fun PickedAudio.readBytes(): ByteArray

internal expect val canPickImageFromDevice: Boolean

internal expect val unplayableMimeTypes: Set<String>

internal fun decodeImageBitmap(bytes: ByteArray): ImageBitmap? =
    runCatching { bytes.decodeToImageBitmap() }.getOrNull()

class PickedImage(
    val name: String,
    val mimeType: String,
    val handle: Any?,
)

@Composable
internal expect fun rememberImagePickerLauncher(onPicked: (PickedImage) -> Unit): () -> Unit

internal expect suspend fun PickedImage.readBytes(): ByteArray

fun mimeTypeForFile(name: String): String {
    val ext = name.substringAfterLast('.', "").lowercase()
    return when (ext) {
        "mp3" -> "audio/mpeg"
        "m4a", "mp4", "m4b", "alac" -> "audio/mp4"
        "aac" -> "audio/aac"
        "flac" -> "audio/flac"
        "wav", "wave" -> "audio/wav"
        "ogg", "oga", "ogx", "opus", "spx" -> "audio/ogg"
        "weba", "webm" -> "audio/webm"
        "wma" -> "audio/x-ms-wma"
        "aiff", "aif" -> "audio/aiff"
        else -> "application/octet-stream"
    }
}
