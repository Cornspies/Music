package com.music.app

import androidx.compose.ui.platform.ClipEntry

/**
 * Wraps plain text for [androidx.compose.ui.platform.Clipboard.setClipEntry].
 *
 * Compose's `Clipboard` API is common, but a [ClipEntry] can only be built
 * from the platform's own clip type (ClipData on Android, a UIPasteboard
 * string on iOS), so the construction lives behind an expect.
 */
internal expect fun plainTextClipEntry(text: String): ClipEntry
