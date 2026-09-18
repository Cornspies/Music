package com.music.app

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.ImageBitmap

/** Whether this platform can upload audio files from the device (Android: yes, iOS: no). */
internal expect val canUploadFromDevice: Boolean

/**
 * A locally picked audio file (Android: content URI resolved to name/size/duration).
 * [handle] holds the platform file reference used by [readBytes].
 */
class PickedAudio(
    val name: String,
    val sizeBytes: Long,
    val durationSec: Double?,
    val handle: Any?,
)

/** Returns a launcher closure — calling it opens the system audio file picker. */
@Composable
internal expect fun rememberAudioPickerLauncher(onPicked: (List<PickedAudio>) -> Unit): () -> Unit

/** Reads the picked file's bytes (may be large — call from a background-friendly context). */
internal expect suspend fun PickedAudio.readBytes(): ByteArray

/** Whether this platform can pick an image from the device (for the avatar). */
internal expect val canPickImageFromDevice: Boolean

/**
 * Formats this platform's player cannot decode, lower-case MIME types.
 *
 * ExoPlayer handles everything the server accepts, so Android's set is empty.
 * AVFoundation has never supported the Ogg container, which is why iOS asks
 * the server for the compatible copy instead.
 */
internal expect val unplayableMimeTypes: Set<String>

/**
 * Decodes encoded image bytes (JPEG/PNG/…) into something Compose can draw,
 * or null when the bytes aren't a readable image.
 *
 * Compose Multiplatform grew a common `ByteArray.decodeToImageBitmap()` in
 * 1.8.0; this project is on 1.7.3, so each platform does it with its own
 * decoder instead.
 */
internal expect fun decodeImageBitmap(bytes: ByteArray): ImageBitmap?

/** A locally picked image. [handle] holds the platform file reference. */
class PickedImage(
    val name: String,
    val mimeType: String,
    val handle: Any?,
)

/** Returns a launcher closure — calling it opens the system image picker. */
@Composable
internal expect fun rememberImagePickerLauncher(onPicked: (PickedImage) -> Unit): () -> Unit

/** Reads the picked image's bytes. */
internal expect suspend fun PickedImage.readBytes(): ByteArray

/**
 * Maps a display file name to a MIME type matching the server's extension whitelist
 * (mp3, m4a, mp4, m4b, alac, aac, flac, wav, wave, ogg, oga, opus, wma, aiff, aif).
 */
fun mimeTypeForFile(name: String): String {
    val ext = name.substringAfterLast('.', "").lowercase()
    return when (ext) {
        "mp3" -> "audio/mpeg"
        "m4a", "mp4", "m4b", "alac" -> "audio/mp4"
        "aac" -> "audio/aac"
        "flac" -> "audio/flac"
        "wav", "wave" -> "audio/wav"
        // .ogx is Ogg multiplexed; the server stores it as audio/ogg too.
        "ogg", "oga", "ogx", "opus", "spx" -> "audio/ogg"
        "weba", "webm" -> "audio/webm"
        "wma" -> "audio/x-ms-wma"
        "aiff", "aif" -> "audio/aiff"
        else -> "application/octet-stream"
    }
}
