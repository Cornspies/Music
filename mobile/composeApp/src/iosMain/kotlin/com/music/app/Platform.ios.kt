package com.music.app

import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.toComposeImageBitmap
import org.jetbrains.skia.Image
import androidx.compose.runtime.Composable

// ---------------------------------------------------------------------------
// Uploads: file picking is not wired up on iOS in this version — the screen
// shows a hint to upload via the web app instead (see UploadScreen).
// ---------------------------------------------------------------------------

internal actual val canUploadFromDevice: Boolean = false

@Composable
internal actual fun rememberAudioPickerLauncher(
    onPicked: (List<PickedAudio>) -> Unit,
): () -> Unit = {
    // No-op on iOS (UI document picker is TODO; upload via the web app).
}

internal actual suspend fun PickedAudio.readBytes(): ByteArray {
    // Never reached: canUploadFromDevice is false on iOS (no picker launcher).
    throw IllegalStateException("not supported on iOS")
}

// ---------------------------------------------------------------------------
// Avatars: image picking is not wired up on iOS in this version, matching the
// upload story above — set your picture from the web app instead.
// ---------------------------------------------------------------------------

internal actual val canPickImageFromDevice: Boolean = false

/**
 * AVFoundation cannot open the Ogg container in any form, and does not handle
 * WebM or WMA. FLAC, ALAC, AAC, MP3, WAV and AIFF are all fine, so lossless
 * audio needs no conversion — only these get swapped for the compatible copy
 * the server made at upload time.
 */
internal actual fun decodeImageBitmap(bytes: ByteArray): ImageBitmap? =
    // Skia is what Compose draws with on iOS, so decode straight to it.
    runCatching { Image.makeFromEncoded(bytes).toComposeImageBitmap() }.getOrNull()

internal actual val unplayableMimeTypes: Set<String> = setOf(
    "audio/ogg",
    "application/ogg",
    "audio/opus",
    "audio/webm",
    "audio/x-ms-wma",
)

@Composable
internal actual fun rememberImagePickerLauncher(
    onPicked: (PickedImage) -> Unit,
): () -> Unit = {
    // No-op on iOS (UIImagePickerController is TODO).
}

internal actual suspend fun PickedImage.readBytes(): ByteArray {
    // Never reached: canPickImageFromDevice is false on iOS.
    throw IllegalStateException("not supported on iOS")
}
