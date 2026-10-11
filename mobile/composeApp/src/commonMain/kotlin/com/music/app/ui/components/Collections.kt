package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import com.music.app.api.HomeMix
import com.music.app.api.Track
import com.music.app.ui.hasArtist
import com.music.app.ui.theme.Zinc400

private val gradients = listOf(
    listOf(Color(0xFF065F46), Color(0xFF18181B)),
    listOf(Color(0xFF3730A3), Color(0xFF18181B)),
    listOf(Color(0xFF9D174D), Color(0xFF18181B)),
    listOf(Color(0xFF92400E), Color(0xFF18181B)),
    listOf(Color(0xFF155E75), Color(0xFF18181B)),
    listOf(Color(0xFF5B21B6), Color(0xFF18181B)),
)

fun seedGradient(seed: String): Brush = Brush.linearGradient(gradients[(seed.hashCode() and Int.MAX_VALUE) % gradients.size])

private fun initials(name: String): String =
    name.split(Regex("\\s+")).filter { it.isNotBlank() }.take(2).joinToString("") { it.take(1).uppercase() }

@Composable
fun ArtistAvatar(name: String, size: Dp, modifier: Modifier = Modifier, picture: String? = null) {
    val model = LocalAppModel.current
    val tracks = model.tracks
    val cover = remember(tracks, name) {
        tracks?.filter { it.hasCover && hasArtist(it.artist, name) }?.maxByOrNull { it.plays }
    }
    Box(
        modifier
            .size(size)
            .clip(CircleShape)
            .background(seedGradient(name)),
        contentAlignment = Alignment.Center,
    ) {
        when {
            !picture.isNullOrBlank() -> AsyncImage(
                model = model.api.catalogImageUrl(picture),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
            cover != null -> CoverImage(cover, Modifier.fillMaxSize(), corner = 0.dp, displaySize = size)
            else -> Text(
                initials(name),
                color = Color.White.copy(alpha = 0.85f),
                fontWeight = FontWeight.Black,
                fontSize = (size.value / 3).sp,
            )
        }
    }
}

@Composable
fun MixCollage(mix: HomeMix, tracksById: Map<String, Track>, modifier: Modifier = Modifier, label: Boolean = true) {
    val covers = mix.coverTrackIds.mapNotNull { tracksById[it] }
    val name = if (mix.kind == "artist") mix.seed ?: mix.title else mix.title
    Box(
        modifier
            .aspectRatio(1f)
            .clip(RoundedCornerShape(12.dp))
            .background(seedGradient(name)),
    ) {
        if (covers.size >= 4) {
            Column(Modifier.fillMaxSize()) {
                for (row in 0 until 2) {
                    Row(Modifier.weight(1f).fillMaxWidth()) {
                        for (col in 0 until 2) {
                            CoverImage(covers[row * 2 + col], Modifier.weight(1f).fillMaxSize(), corner = 0.dp)
                        }
                    }
                }
            }
        } else if (covers.isNotEmpty()) {
            CoverImage(covers[0], Modifier.fillMaxSize(), corner = 0.dp)
        }
        Box(Modifier.fillMaxSize().background(seedGradient(name), alpha = 0.35f))
        if (label) {
            Column(
                Modifier
                    .align(Alignment.BottomStart)
                    .fillMaxWidth()
                    .background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.8f))))
                    .padding(start = 10.dp, end = 10.dp, top = 20.dp, bottom = 8.dp),
            ) {
                Text(
                    if (mix.kind == "artist") "MIX" else "FOR YOU",
                    color = Color.White.copy(alpha = 0.7f),
                    fontSize = 9.sp,
                    fontWeight = FontWeight.SemiBold,
                    letterSpacing = 2.sp,
                )
                Text(
                    name,
                    color = Color.White,
                    fontWeight = FontWeight.Black,
                    fontSize = 17.sp,
                    lineHeight = 19.sp,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
fun ArtistCircle(name: String, caption: String, picture: String?, onClick: () -> Unit) {
    Column(
        Modifier
            .width(112.dp)
            .clip(RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        ArtistAvatar(name, 100.dp, picture = picture)
        Spacer(Modifier.height(8.dp))
        Text(
            name,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            caption,
            style = MaterialTheme.typography.bodySmall,
            color = Zinc400,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
fun MixCard(mix: HomeMix, tracksById: Map<String, Track>, onClick: () -> Unit) {
    Column(
        Modifier
            .width(148.dp)
            .clip(RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(4.dp),
    ) {
        MixCollage(mix, tracksById, Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        Text(
            mix.title,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            mix.subtitle,
            style = MaterialTheme.typography.bodySmall,
            color = Zinc400,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
