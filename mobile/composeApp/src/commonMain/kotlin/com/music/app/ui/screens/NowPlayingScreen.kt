package com.music.app.ui.screens

import com.music.app.ui.components.GoToItems
import androidx.compose.material3.DropdownMenu
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
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
import androidx.compose.material.icons.filled.Groups
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.Track
import com.music.app.state.RepeatMode
import com.music.app.state.ShuffleMode
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.LyricsScreen
import com.music.app.ui.components.LyricsStrip
import com.music.app.ui.components.WaveformBar
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.OnEmerald
import com.music.app.ui.theme.Zinc400
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

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
    val position by model.player.positionSec.collectAsStateWithLifecycle()

    var dragValue by remember { mutableStateOf<Double?>(null) }
    val duration = track.duration
    val shownPosition = dragValue ?: position

    LaunchedEffect(track.id, track.hasWaveform) { model.loadWaveform(track) }
    val waveform = model.waveformFor(track)

    val scope = rememberCoroutineScope()
    val dragOffset = remember { Animatable(0f) }
    val dismissThresholdPx = with(LocalDensity.current) { 140.dp.toPx() }

    val coverOffset = remember { Animatable(0f) }
    val skipThresholdPx = with(LocalDensity.current) { 80.dp.toPx() }
    var skipping by remember { mutableStateOf(false) }
    var lyricsOpen by remember { mutableStateOf(false) }
    val session = model.listen.session
    val canSkip = session?.canControl ?: true
    LaunchedEffect(track.id) { model.loadLyrics(track) }
    val lyrics = model.lyricsFor(track)?.takeIf { it.found && !(it.instrumental && it.plain.isBlank()) }

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
                        scope.launch { dragOffset.snapTo((dragOffset.value + dy).coerceAtLeast(0f)) }
                    },
                )
            }
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Back")
            }
            Text(
                if (session != null) {
                    val people = session.members.size
                    "Listening together · $people ${if (people == 1) "person" else "people"}"
                } else {
                    "Now playing"
                },
                style = MaterialTheme.typography.labelLarge,
                color = if (session != null) Emerald else Zinc400,
                modifier = Modifier.weight(1f),
            )
            IconButton(onClick = { model.push(Screen.Listen) }) {
                Icon(
                    Icons.Filled.Groups,
                    contentDescription = "Listen together",
                    tint = if (session != null) Emerald else Zinc400,
                )
            }
        }

        BoxWithConstraints(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(vertical = 12.dp),
            contentAlignment = Alignment.Center,
        ) {
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
                                        model.previous(restartFirst = false)
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

        if (lyrics != null) {
            LyricsStrip(lyrics, position = position, onClick = { lyricsOpen = true })
            Spacer(Modifier.height(10.dp))
        }

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
                var goToOpen by remember(track.id) { mutableStateOf(false) }
                Box {
                    Text(
                        track.artist.ifBlank { "Unknown artist" } +
                            (track.album.takeIf { it.isNotBlank() }?.let { " · $it" } ?: ""),
                        style = MaterialTheme.typography.bodyMedium,
                        color = Zinc400,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.clickable { goToOpen = true },
                    )
                    DropdownMenu(expanded = goToOpen, onDismissRequest = { goToOpen = false }) {
                        GoToItems(track) { goToOpen = false }
                    }
                }
            }
            IconButton(onClick = { model.toggleFavorite(track) }) {
                Icon(
                    if (track.isFavorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                    contentDescription = if (track.isFavorite) "Remove from favorites" else "Add to favorites",
                    tint = if (track.isFavorite) Emerald else Zinc500,
                )
            }
        }

        Spacer(Modifier.height(8.dp))

        SeekBar(
            waveform = waveform,
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

        Row(
            Modifier
                .fillMaxWidth()
                .padding(bottom = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceEvenly,
        ) {
            IconButton(onClick = { model.cycleShuffle() }, enabled = session == null) {
                val tint = if (model.shuffle && session == null) Emerald else Zinc500
                Box {
                    Icon(
                        Icons.Filled.Shuffle,
                        contentDescription = when (model.shuffleMode) {
                            ShuffleMode.OFF -> "Shuffle off"
                            ShuffleMode.RANDOM -> "Shuffle on"
                            ShuffleMode.NO_REPEAT -> "Shuffle without repeats"
                        },
                        tint = tint,
                    )
                    if (model.shuffleMode == ShuffleMode.NO_REPEAT) {
                        Box(
                            Modifier
                                .align(Alignment.TopEnd)
                                .offset(x = 3.dp, y = (-3).dp)
                                .size(6.dp)
                                .background(tint, CircleShape),
                        )
                    }
                }
            }
            IconButton(onClick = { model.previous() }, enabled = canSkip) {
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
            IconButton(onClick = { model.next() }, enabled = canSkip) {
                Icon(
                    Icons.Filled.SkipNext,
                    contentDescription = "Next track",
                    modifier = Modifier.size(34.dp),
                )
            }
            IconButton(enabled = session == null, onClick = {
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
                    tint = if (model.repeatMode != RepeatMode.OFF && session == null) Emerald else Zinc500,
                )
            }
        }
    }

    AnimatedVisibility(
        visible = lyricsOpen,
        enter = slideInVertically(tween(260)) { it } + fadeIn(tween(200)),
        exit = slideOutVertically(tween(220)) { it } + fadeOut(tween(180)),
    ) {
        LyricsScreen(track, onClose = { lyricsOpen = false })
    }
}

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
