package com.music.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
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
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.state.RepeatMode
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.EmptyState
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.OnEmerald
import com.music.app.ui.theme.Zinc400
import com.music.app.ui.theme.Zinc500

/** Full-screen player: big cover, seek slider, prev/play/next, shuffle & repeat. */
@Composable
fun NowPlayingScreen() {
    val model = LocalAppModel.current
    val track = model.currentTrack
    if (track == null) {
        EmptyState(
            Icons.Filled.MusicNote,
            "Nothing playing",
            subtitle = "Pick a track from your library or playlists.",
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

    var dragging by remember { mutableStateOf(false) }
    var dragValue by remember { mutableStateOf(0f) }

    val duration = track.duration
    val sliderValue = when {
        dragging -> dragValue
        duration > 0.0 -> position.toFloat().coerceIn(0f, duration.toFloat())
        else -> 0f
    }
    val shownPosition = if (dragging) dragValue.toDouble() else position

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // ---- header ----
        Row(
            Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Text(
                "Now playing",
                style = MaterialTheme.typography.titleMedium,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.size(48.dp)) // balances the back button
        }

        Spacer(Modifier.height(20.dp))

        // ---- cover ----
        CoverImage(
            track,
            Modifier
                .fillMaxWidth()
                .aspectRatio(1f),
            corner = 24.dp,
        )

        Spacer(Modifier.height(28.dp))

        // ---- title / artist / favorite ----
        Text(
            track.title,
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
            textAlign = TextAlign.Center,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            track.artist.ifBlank { "Unknown artist" },
            style = MaterialTheme.typography.bodyLarge,
            color = Zinc400,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        IconButton(onClick = { model.toggleFavorite(track) }) {
            Icon(
                if (track.isFavorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                contentDescription = if (track.isFavorite) "Remove from favorites" else "Add to favorites",
                tint = if (track.isFavorite) Emerald else Zinc500,
            )
        }

        Spacer(Modifier.height(8.dp))

        // ---- seek ----
        Slider(
            value = sliderValue,
            onValueChange = {
                dragValue = it
                dragging = true
            },
            onValueChangeFinished = {
                model.seekTo(dragValue.toDouble())
                dragging = false
            },
            valueRange = 0f..(if (duration > 0.0) duration.toFloat() else 1f),
            enabled = duration > 0.0,
            modifier = Modifier.fillMaxWidth(),
        )
        Row(Modifier.fillMaxWidth()) {
            Text(
                formatDuration(shownPosition),
                style = MaterialTheme.typography.bodySmall,
                color = Zinc400,
            )
            Spacer(Modifier.weight(1f))
            Text(
                formatDuration(duration),
                style = MaterialTheme.typography.bodySmall,
                color = Zinc400,
                textAlign = TextAlign.Right,
            )
        }

        Spacer(Modifier.height(20.dp))

        // ---- transport controls ----
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.Center,
            modifier = Modifier.fillMaxWidth(),
        ) {
            IconButton(onClick = { model.previous() }) {
                Icon(
                    Icons.Filled.SkipPrevious,
                    contentDescription = "Previous track",
                    modifier = Modifier.size(36.dp),
                )
            }
            Spacer(Modifier.width(28.dp))
            Box(
                Modifier
                    .size(72.dp)
                    .clip(CircleShape)
                    .background(Emerald)
                    .clickable { model.togglePlayPause() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                    contentDescription = if (isPlaying) "Pause" else "Play",
                    tint = OnEmerald,
                    modifier = Modifier.size(34.dp),
                )
            }
            Spacer(Modifier.width(28.dp))
            IconButton(onClick = { model.next() }) {
                Icon(
                    Icons.Filled.SkipNext,
                    contentDescription = "Next track",
                    modifier = Modifier.size(36.dp),
                )
            }
        }

        Spacer(Modifier.height(12.dp))

        // ---- shuffle / repeat ----
        Row(
            horizontalArrangement = Arrangement.spacedBy(40.dp),
            modifier = Modifier.padding(top = 4.dp),
        ) {
            IconButton(onClick = { model.shuffle = !model.shuffle }) {
                Icon(
                    Icons.Filled.Shuffle,
                    contentDescription = "Shuffle",
                    tint = if (model.shuffle) Emerald else Zinc500,
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

        Spacer(Modifier.height(32.dp))
    }
}
