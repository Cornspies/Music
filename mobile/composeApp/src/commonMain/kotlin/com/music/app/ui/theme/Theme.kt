package com.music.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// Emerald accent on near-black zinc — the Music design language (no blue/indigo).
val Emerald = Color(0xFF34D399) // emerald-400 — primary
val EmeraldLight = Color(0xFF6EE7B7)
val EmeraldDark = Color(0xFF059669) // emerald-600
val OnEmerald = Color(0xFF022C22) // deep emerald-950

val Zinc50 = Color(0xFFFAFAFA)
val Zinc100 = Color(0xFFF4F4F5)
val Zinc400 = Color(0xFFA1A1AA)
val Zinc500 = Color(0xFF71717A)
val Zinc600 = Color(0xFF52525B)
val Zinc700 = Color(0xFF3F3F46)
val Zinc800 = Color(0xFF27272A)
val Zinc900 = Color(0xFF18181B)
val Zinc950 = Color(0xFF09090B)

val ErrorRed = Color(0xFFF87171)

private val MusicDarkColors = darkColorScheme(
    primary = Emerald,
    onPrimary = OnEmerald,
    primaryContainer = Color(0xFF064E3B),
    onPrimaryContainer = Color(0xFFD1FAE5),
    secondary = EmeraldDark,
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFF065F46),
    onSecondaryContainer = Color(0xFFA7F3D0),
    tertiary = EmeraldLight,
    onTertiary = OnEmerald,
    background = Zinc950,
    onBackground = Zinc100,
    surface = Zinc900,
    onSurface = Zinc100,
    surfaceVariant = Zinc800,
    onSurfaceVariant = Zinc400,
    surfaceTint = Emerald,
    inverseSurface = Zinc100,
    inverseOnSurface = Zinc900,
    outline = Zinc600,
    outlineVariant = Zinc700,
    error = ErrorRed,
    onError = Color(0xFF450A0A),
    errorContainer = Color(0xFF7F1D1D),
    onErrorContainer = Color(0xFFFCA5A5),
    scrim = Color(0xFF000000),
)

/** Music is a always-dark, emerald-on-zinc app. */
@Composable
fun MusicTheme(content: @Composable () -> Unit) {
    // Dark regardless of system setting — a music app at night.
    isSystemInDarkTheme() // kept for future light-mode support
    MaterialTheme(
        colorScheme = MusicDarkColors,
        content = content,
    )
}
