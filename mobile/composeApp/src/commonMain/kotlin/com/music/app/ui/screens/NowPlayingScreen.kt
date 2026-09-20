package com.music.app.ui.screens

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.state.RepeatMode
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.LyricsSheet
import com.music.app.ui.components.LyricsStrip
import com.music.app.ui.components.WaveformBar
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.OnEmerald
import com.music.app.ui.theme.Zinc400
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/**
 * Full-screen player.
 *
 * Swipe down anywhere to close; swipe the cover left or right to skip to the
 * next or previous track. When the track has lyrics, the line being sung sits
 * between the title and the seek bar and opens the full text. Laid out top to bottom with no scrolling: the cover takes whatever height is
 * left after everything else has been placed, so on a short phone it simply
 * shrinks rather than pushing the controls off the bottom. The waveform (when
 * the server has analysed the track) is the seek bar itself — tap or drag it;
 * a plain slider stands in until then.
 */
@Composable
fun NowPlayingScreen() {
    val model = LocalAppModel.current
    val track = model.currentTrack
    if (track == null) {
        EmptyState(
            Icons.Filled.MusicNote,
            "Nothing playing",
        )
        return
    }

    NowPlayingContent(track)
}

@Composable
private fun NowPlayingContent(track: Track) {
    val model = LocalAppModel.current
    val isPlaying by model.player.isPlaying.collectAsState()
    val position by model.player.positionSec.collectAsState()

    // While the finger is down the bar follows it, not the player.
    var dragValue by remember { mutableStateOf<Double?>(null) }
    val duration = track.duration
    val shownPosition = dragValue ?: position

    LaunchedEffect(track.id, track.hasWaveform) { model.loadWaveform(track) }
    val waveform = model.waveformFor(track)

    // Swipe down to close, the way every full-screen player does: the content
    // follows the finger, and past a third of the screen letting go dismisses
    // it; short of that it springs back. Only vertical drags are claimed, so
    // scrubbing the waveform sideways still works.
    val scope = rememberCoroutineScope()
    val dragOffset = remember { Animatable(0f) }
    val dismissThresholdPx = with(LocalDensity.current) { 140.dp.toPx() }

    // Swipe the cover sideways to skip: left for the next track, right for the
    // previous one. The art follows the finger, slides off the edge, and the
    // new track's art slides in from the opposite side.
    val coverOffset = remember { Animatable(0f) }
    val skipThresholdPx = with(LocalDensity.current) { 80.dp.toPx() }
    var skipping by remember { mutableStateOf(false) }
    var lyricsOpen by remember { mutableStateOf(false) }
    LaunchedEffect(track.id) { model.loadLyrics(track) }
    val lyrics = model.lyricsFor(track)?.takeIf { it.found && !(it.instrumental && it.plain.isBlank()) }

    // The background travels with the content, so what's underneath shows
    // through as the player slides away.
    Column(
        Modifier
            .fillMaxSize()
            .offset { IntOffset(0, dragOffset.value.roundToInt()) }
            .background(MaterialTheme.colorScheme.background)
            .systemBarsPadding()
            .pointerInput(Unit) {
                detectVerticalDragGestures(
                    onDragEnd = {
                        if (dragOffset.value > dismissThresholdPx) {
                            scope.launch {
                                dragOffset.animateTo(size.height.toFloat(), tween(180))
                                model.pop()
                            }
                        } else {
                            scope.launch { dragOffset.animateTo(0f, spring(stiffness = Spring.StiffnessMedium)) }
                        }
                    },
                    onDragCancel = {
                        scope.launch { dragOffset.animateTo(0f) }
                    },
                    onVerticalDrag = { change, dy ->
                        change.consume()
                        // Upward drags do nothing; downward ones track the finger.
                        scope.launch { dragOffset.snapTo((dragOffset.value + dy).coerceAtLeast(0f)) }
                    },
                )
            }
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // ---- header: collapse chevron, nothing else competing for height ----
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Back")
            }
            Text(
                "Now playing",
                style = MaterialTheme.typography.labelLarge,
                color = Zinc400,
                modifier = Modifier.weight(1f),
            )
        }

        // ---- cover: a true square, as large as the leftover room allows ----
        BoxWithConstraints(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(vertical = 12.dp),
            contentAlignment = Alignment.Center,
        ) {
            // Measured explicitly rather than with aspectRatio(): inside a
            // weighted slot the constraints are tight, and aspectRatio can't
            // make a square out of those — the art ended up stretched and
            // cropped.
            val edge = minOf(maxWidth, maxHeight)
            val widthPx = constraints.maxWidth.toFloat()
            CoverImage(
                track,
                Modifier
                    .size(edge)
                    .offset { IntOffset(coverOffset.value.roundToInt(), 0) }
                    .pointerInput(Unit) {
                        detectHorizontalDragGestures(
                            onDragEnd = {
                                if (skipping) return@detectHorizontalDragGestures
                                val dx = coverOffset.value
                                when {
                                    dx < -skipThresholdPx -> scope.launch {
                                        skipping = true
                                        coverOffset.animateTo(-widthPx, tween(150))
                                        model.next()
                                        coverOffset.snapTo(widthPx)
                                        coverOffset.animateTo(0f, tween(200))
                                        skipping = false
                                    }
                                    dx > skipThresholdPx -> scope.launch {
                                        skipping = true
                                        coverOffset.animateTo(widthPx, tween(150))
                                        model.previous()
                                        coverOffset.snapTo(-widthPx)
                                        coverOffset.animateTo(0f, tween(200))
                                        skipping = false
                                    }
                                    else -> scope.launch {
                                        coverOffset.animateTo(0f, spring(stiffness = Spring.StiffnessMedium))
                                    }
                                }
                            },
                            onDragCancel = { scope.launch { coverOffset.animateTo(0f) } },
                            onHorizontalDrag = { change, dx ->
                                change.consume()
                                if (!skipping) scope.launch { coverOffset.snapTo(coverOffset.value + dx) }
                            },
                        )
                    },
                corner = 20.dp,
                placeholderSize = edge / 3,
            )
        }

        // ---- title / artist with the heart alongside, not below ----
        Row(
            Modifier
                .fillMaxWidth()
                .padding(top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    track.title,
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    track.artist.ifBlank { "Unknown artist" } +
                        (track.album.takeIf { it.isNotBlank() }?.let { " · $it" } ?: ""),
                    style = MaterialTheme.typography.bodyMedium,
                    color = Zinc400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            IconButton(onClick = { model.toggleFavorite(track) }) {
                Icon(
                    if (track.isFavorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                    contentDescription = if (track.isFavorite) "Remove from favorites" else "Add to favorites",
                    tint = if (track.isFavorite) Emerald else Zinc500,
                )
            }
        }

        // ---- lyrics: the line being sung, between the art and the transport;
        // tap for the whole text. Absent when the track has none, and the
        // cover takes the room back.
        if (lyrics != null) {
            Spacer(Modifier.height(10.dp))
            LyricsStrip(lyrics, position = position, onClick = { lyricsOpen = true })
        }

        Spacer(Modifier.height(8.dp))

        // ---- seek ----
        SeekBar(
            waveform = waveform,
            // A track the server has analysed gets a waveform-shaped
            // placeholder while its peaks arrive, not the slider: swapping
            // slider → waveform a moment after every skip looked like a glitch.
            expectWaveform = track.hasWaveform,
            position = shownPosition,
            duration = duration,
            onPreview = { dragValue = it },
            onSeek = {
                model.seekTo(it)
                dragValue = null
            },
        )
        Row(Modifier.fillMaxWidth()) {
            Text(formatDuration(shownPosition), style = MaterialTheme.typography.labelSmall, color = Zinc400)
            Spacer(Modifier.weight(1f))
            Text(formatDuration(duration), style = MaterialTheme.typography.labelSmall, color = Zinc400)
        }

        Spacer(Modifier.height(8.dp))

        // ---- one row of controls: shuffle · prev · play · next · repeat ----
        Row(
            Modifier
                .fillMaxWidth()
                .padding(bottom = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceEvenly,
        ) {
            IconButton(onClick = { model.shuffle = !model.shuffle }) {
                Icon(
                    Icons.Filled.Shuffle,
                    contentDescription = "Shuffle",
                    tint = if (model.shuffle) Emerald else Zinc500,
                )
            }
            IconButton(onClick = { model.previous() }) {
                Icon(
                    Icons.Filled.SkipPrevious,
                    contentDescription = "Previous track",
                    modifier = Modifier.size(34.dp),
                )
            }
            Box(
                Modifier
                    .size(64.dp)
                    .clip(CircleShape)
                    .background(Emerald)
                    .clickable { model.togglePlayPause() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                    contentDescription = if (isPlaying) "Pause" else "Play",
                    tint = OnEmerald,
                    modifier = Modifier.size(32.dp),
                )
            }
            IconButton(onClick = { model.next() }) {
                Icon(
                    Icons.Filled.SkipNext,
                    contentDescription = "Next track",
                    modifier = Modifier.size(34.dp),
                )
            }
            IconButton(onClick = {
                model.repeatMode = when (model.repeatMode) {
                    RepeatMode.OFF -> RepeatMode.ALL
                    RepeatMode.ALL -> RepeatMode.ONE
                    RepeatMode.ONE -> RepeatMode.OFF
                }
            }) {
                Icon(
                    if (model.repeatMode == RepeatMode.ONE) Icons.Filled.RepeatOne else Icons.Filled.Repeat,
                    contentDescription = when (model.repeatMode) {
                        RepeatMode.OFF -> "Repeat off"
                        RepeatMode.ALL -> "Repeat queue"
                        RepeatMode.ONE -> "Repeat track"
                    },
                    tint = if (model.repeatMode != RepeatMode.OFF) Emerald else Zinc500,
                )
            }
        }
    }

    if (lyricsOpen) {
        LyricsSheet(track, onDismiss = { lyricsOpen = false })
    }
}

/**
 * The waveform as a seek bar: tap anywhere to jump, drag to scrub. Without a
 * waveform, a standard slider does the same job at the same height so the
 * layout doesn't jump when the analysis arrives. With [expectWaveform] but no
 * peaks yet, a flat bar of the same shape stands in until they load.
 */
/** Low, even bars: what a waveform looks like before its peaks have arrived. */
private val PLACEHOLDER_PEAKS: List<Int> = List(200) { 6 }

@Composable
private fun SeekBar(
    waveform: List<Int>?,
    expectWaveform: Boolean,
    position: Double,
    duration: Double,
    onPreview: (Double) -> Unit,
    onSeek: (Double) -> Unit,
) {
    val enabled = duration > 0.0
    if (waveform != null || expectWaveform) {
        Box(
            Modifier
                .fillMaxWidth()
                .height(48.dp)
                .pointerInput(enabled, duration) {
                    if (!enabled) return@pointerInput
                    detectTapGestures { offset ->
                        onSeek((offset.x / size.width).coerceIn(0f, 1f) * duration)
                    }
                }
                .pointerInput(enabled, duration) {
                    if (!enabled) return@pointerInput
                    var x = 0f
                    detectHorizontalDragGestures(
                        onDragStart = { offset ->
                            x = offset.x
                            onPreview((x / size.width).coerceIn(0f, 1f) * duration)
                        },
                        onDragEnd = { onSeek((x / size.width).coerceIn(0f, 1f) * duration) },
                        onDragCancel = { onSeek((x / size.width).coerceIn(0f, 1f) * duration) },
                        onHorizontalDrag = { change, dragAmount ->
                            change.consume()
                            x = (x + dragAmount).coerceIn(0f, size.width.toFloat())
                            onPreview((x / size.width) * duration)
                        },
                    )
                },
        ) {
            WaveformBar(
                peaks = waveform ?: PLACEHOLDER_PEAKS,
                fraction = if (enabled) (position / duration).toFloat().coerceIn(0f, 1f) else 0f,
                modifier = Modifier.fillMaxSize(),
            )
        }
    } else {
        Box(Modifier.fillMaxWidth().height(48.dp), contentAlignment = Alignment.Center) {
            Slider(
                value = if (enabled) position.toFloat().coerceIn(0f, duration.toFloat()) else 0f,
                onValueChange = { onPreview(it.toDouble()) },
                onValueChangeFinished = { onSeek(position) },
                valueRange = 0f..(if (enabled) duration.toFloat() else 1f),
                enabled = enabled,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
