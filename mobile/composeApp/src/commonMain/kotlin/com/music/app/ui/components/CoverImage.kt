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
import coil3.compose.LocalPlatformContext
import coil3.request.ImageRequest
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.ui.theme.EmeraldDark
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc800

@Composable
fun CoverImage(
    track: Track,
    modifier: Modifier = Modifier,
    corner: Dp = 8.dp,
    placeholderSize: Dp = 20.dp,
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
            val cacheKey = "cover:${track.id}:${track.updatedAt}"
            AsyncImage(
                model = ImageRequest.Builder(LocalPlatformContext.current)
                    .data(model.api.coverUrl(track.id))
                    .memoryCacheKey(cacheKey)
                    .diskCacheKey(cacheKey)
                    .build(),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Icon(
                imageVector = Icons.Filled.MusicNote,
                contentDescription = null,
                tint = Zinc500,
                modifier = Modifier.size(placeholderSize),
            )
        }
    }
}

@Composable
fun CoverImage(track: Track, size: Dp, modifier: Modifier = Modifier, corner: Dp = 8.dp) {
    CoverImage(track, modifier.size(size), corner)
}
