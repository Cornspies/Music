package com.music.app.ui.theme

import androidx.compose.ui.graphics.Color

fun lyricsSurface(coverColor: String): Color {
    val hex = coverColor.trim().removePrefix("#")
    val n = hex.takeIf { it.length == 6 }?.toIntOrNull(16)
    var r = if (n != null) ((n shr 16) and 255) / 255f else 63 / 255f
    var g = if (n != null) ((n shr 8) and 255) / 255f else 63 / 255f
    var b = if (n != null) (n and 255) / 255f else 70 / 255f
    val lum = 0.2126f * r + 0.7152f * g + 0.0722f * b
    when {
        lum > 0.32f -> {
            val k = 0.32f / lum
            r *= k; g *= k; b *= k
        }
        lum < 0.12f -> {
            val t = (0.12f - lum) / (1f - lum)
            r += (1f - r) * t; g += (1f - g) * t; b += (1f - b) * t
        }
    }
    return Color(r, g, b)
}
