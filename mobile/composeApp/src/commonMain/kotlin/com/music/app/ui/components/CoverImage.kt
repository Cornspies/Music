package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.ui.theme.EmeraldDark
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc800

/**
 * Track cover art. Loads `/api/tracks/{id}/cover?token=…` via Coil when the track has
 * embedded art; otherwise (or while/if the fetch fails) a gradient tile with a music note
 * shows through.
 */
@Composable
fun CoverImage(
    track: Track,
    modifier: Modifier = Modifier,
    corner: Dp = 8.dp,
) {
    val model = LocalAppModel.current
    val gradient = Brush.linearGradient(listOf(Zinc800, EmeraldDark.copy(alpha = 0.35f)))
    Box(
        modifier
            .clip(RoundedCornerShape(corner))
            .background(gradient),
        contentAlignment = Alignment.Center,
    ) {
        if (track.hasCover) {
            AsyncImage(
                model = model.api.coverUrl(track.id),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Icon(
                imageVector = Icons.Filled.MusicNote,
                contentDescription = null,
                tint = Zinc500,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}

/** Convenience overload with an explicit square size. */
@Composable
fun CoverImage(track: Track, size: Dp, modifier: Modifier = Modifier, corner: Dp = 8.dp) {
    CoverImage(track, modifier.size(size), corner)
}
