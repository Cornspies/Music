@file:OptIn(ExperimentalTime::class)

package com.music.app.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Lyrics
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.LyricLine
import com.music.app.api.Track
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.lyricsSurface
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

private fun activeIndex(lines: List<LyricLine>, position: Double): Int {
    var idx = -1
    for ((i, l) in lines.withIndex()) {
        if (l.t <= position + 0.15) idx = i else break
    }
    return idx
}

private fun sourceLabel(source: String): String = when (source) {
    "embedded" -> "From the file"
    "lrclib" -> "From LRCLIB"
    "manual" -> "Added by hand"
    else -> ""
}

@Composable
fun LyricsScreen(track: Track, onClose: () -> Unit) {
    val model = LocalAppModel.current
    val isPlaying by model.player.isPlaying.collectAsState()
    val position by model.player.positionSec.collectAsState()

    BackHandler(enabled = true, onBack = onClose)
    LaunchedEffect(track.id) { model.loadLyrics(track) }
    val lyrics = model.lyricsFor(track)
    val synced = lyrics?.synced?.takeIf { it.isNotEmpty() }
    val current = if (synced != null) activeIndex(synced, position) else -1

    val surface by animateColorAsState(lyricsSurface(track.coverColor), tween(500), label = "lyrics surface")
    val dim = Color.White.copy(alpha = 0.45f)

    Column(
        Modifier
            .fillMaxSize()
            .background(Brush.verticalGradient(listOf(surface, surface.copy(red = surface.red * 0.55f, green = surface.green * 0.55f, blue = surface.blue * 0.55f))))
            .systemBarsPadding(),
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 16.dp, top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onClose) {
                Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Close lyrics", tint = Color.White)
            }
            CoverImage(track, Modifier.size(44.dp), corner = 6.dp)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    track.title,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold,
                    color = Color.White,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    track.artist.ifBlank { "Unknown artist" },
                    style = MaterialTheme.typography.bodySmall,
                    color = dim,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        Box(Modifier.weight(1f).fillMaxWidth()) {
            when {
                lyrics == null -> Centered("Loading lyrics…", dim)
                !lyrics.found -> Centered("No lyrics found", dim, icon = true)
                lyrics.instrumental && lyrics.plain.isBlank() -> Centered("Instrumental", dim)
                synced != null -> SyncedLines(synced, current, dim) { model.seekTo(it) }
                else -> Text(
                    lyrics.plain,
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.ExtraBold,
                    color = Color.White.copy(alpha = 0.9f),
                    lineHeight = MaterialTheme.typography.headlineSmall.lineHeight * 1.25,
                    modifier = Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = 24.dp, vertical = 32.dp),
                )
            }
        }
        if (lyrics != null && lyrics.found) {
            Text(
                sourceLabel(lyrics.source),
                style = MaterialTheme.typography.labelSmall,
                color = dim,
                modifier = Modifier.padding(horizontal = 24.dp),
            )
        }

        val duration = track.duration
        var dragValue by remember { mutableStateOf<Double?>(null) }
        val shown = dragValue ?: position
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
            Slider(
                value = if (duration > 0) shown.toFloat().coerceIn(0f, duration.toFloat()) else 0f,
                onValueChange = { dragValue = it.toDouble() },
                onValueChangeFinished = {
                    dragValue?.let { model.seekTo(it) }
                    dragValue = null
                },
                valueRange = 0f..(if (duration > 0) duration.toFloat() else 1f),
                enabled = duration > 0,
                colors = SliderDefaults.colors(
                    thumbColor = Color.White,
                    activeTrackColor = Color.White,
                    inactiveTrackColor = Color.White.copy(alpha = 0.3f),
                ),
                modifier = Modifier.fillMaxWidth().height(28.dp),
            )
            Row(Modifier.fillMaxWidth()) {
                Text(formatDuration(shown), style = MaterialTheme.typography.labelSmall, color = dim)
                Spacer(Modifier.weight(1f))
                Text(formatDuration(duration), style = MaterialTheme.typography.labelSmall, color = dim)
            }
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.Center,
            ) {
                IconButton(onClick = { model.previous() }) {
                    Icon(Icons.Filled.SkipPrevious, contentDescription = "Previous track", tint = Color.White, modifier = Modifier.size(32.dp))
                }
                Spacer(Modifier.width(16.dp))
                Box(
                    Modifier
                        .size(56.dp)
                        .clip(CircleShape)
                        .background(Color.White)
                        .clickable { model.togglePlayPause() },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                        contentDescription = if (isPlaying) "Pause" else "Play",
                        tint = surface.copy(red = surface.red * 0.5f, green = surface.green * 0.5f, blue = surface.blue * 0.5f),
                        modifier = Modifier.size(30.dp),
                    )
                }
                Spacer(Modifier.width(16.dp))
                IconButton(onClick = { model.next() }) {
                    Icon(Icons.Filled.SkipNext, contentDescription = "Next track", tint = Color.White, modifier = Modifier.size(32.dp))
                }
            }
        }
    }
}

@Composable
private fun SyncedLines(lines: List<LyricLine>, current: Int, dim: Color, onSeek: (Double) -> Unit) {
    val listState = rememberLazyListState()

    var lastUserScroll by remember { mutableLongStateOf(0L) }
    var following by remember { mutableStateOf(false) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress }.collect { scrolling ->
            if (scrolling && !following) lastUserScroll = Clock.System.now().toEpochMilliseconds()
        }
    }
    LaunchedEffect(current) {
        if (current < 0) return@LaunchedEffect
        if (Clock.System.now().toEpochMilliseconds() - lastUserScroll < 4000) return@LaunchedEffect
        val viewport = listState.layoutInfo.viewportSize.height
        following = true
        try {
            listState.animateScrollToItem(current, scrollOffset = -viewport / 3)
        } finally {
            following = false
        }
    }

    LazyColumn(
        state = listState,
        contentPadding = PaddingValues(top = 120.dp, bottom = 240.dp, start = 16.dp, end = 16.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(lines) { i, l ->
            val color by animateColorAsState(if (i == current) Color.White else dim, tween(250), label = "line")
            Text(
                l.line.ifBlank { "♪" },
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.ExtraBold,
                color = color,
                lineHeight = MaterialTheme.typography.headlineSmall.lineHeight * 1.15,
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { onSeek(l.t) }
                    .padding(horizontal = 8.dp, vertical = 10.dp),
            )
        }
    }
}

@Composable
private fun Centered(text: String, color: Color, icon: Boolean = false) {
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        if (icon) {
            Icon(Icons.Filled.Lyrics, contentDescription = null, tint = color)
            Spacer(Modifier.height(8.dp))
        }
        Text(text, style = MaterialTheme.typography.bodyLarge, color = color)
    }
}
