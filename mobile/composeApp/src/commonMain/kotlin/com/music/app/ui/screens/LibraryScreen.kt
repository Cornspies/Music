package com.music.app.ui.screens

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CloudUpload
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.Track
import com.music.app.api.friendlyError
import kotlinx.coroutines.launch
import kotlin.math.roundToInt
import com.music.app.ui.components.DeleteTrackDialog
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.TrackRow

private val SORT_OPTIONS = listOf(
    "created" to "Recently added",
    "title" to "Title",
    "artist" to "Artist",
    "plays" to "Most played",
    "recent" to "Recently played",
)

@Composable
fun LibraryScreen() {
    val model = LocalAppModel.current
    LaunchedEffect(Unit) { model.loadLibrary() }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    "Library",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "${model.tracks?.size ?: 0} tracks",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            var sortMenuOpen by remember { mutableStateOf(false) }
            Box {
                IconButton(onClick = { sortMenuOpen = true }) {
                    Icon(Icons.AutoMirrored.Filled.Sort, contentDescription = "Sort")
                }
                DropdownMenu(
                    expanded = sortMenuOpen,
                    onDismissRequest = { sortMenuOpen = false },
                ) {
                    SORT_OPTIONS.forEach { (value, label) ->
                        DropdownMenuItem(
                            text = { Text(label) },
                            onClick = {
                                sortMenuOpen = false
                                model.setLibrarySort(value)
                            },
                            trailingIcon = {
                                if (model.tracksSort == value) {
                                    Icon(Icons.Filled.Check, contentDescription = null)
                                }
                            },
                        )
                    }
                }
            }
            IconButton(onClick = { model.push(Screen.Upload) }) {
                Icon(Icons.Filled.CloudUpload, contentDescription = "Upload music")
            }
        }

        val tracks = model.tracks
        when {
            model.tracksLoading && tracks == null -> LoadingBox()
            model.tracksError != null && tracks == null ->
                ErrorState(model.tracksError ?: "Could not load library") { model.loadLibrary() }
            tracks.isNullOrEmpty() -> EmptyState(
                Icons.Filled.MusicNote,
                "No music yet",
            )
            else -> RefreshableContent(
                isRefreshing = model.tracksLoading,
                onRefresh = { model.loadLibrary() },
            ) { TrackList(tracks) }
        }
    }
}

@Composable
private fun TrackList(tracks: List<Track>) {
    LazyColumn(Modifier.fillMaxSize()) {
        items(tracks, key = { it.id }) { track ->
            SwipeToDeleteTrack(track) { TrackRow(track, tracks) }
        }
    }
}

@Composable
private fun SwipeToDeleteTrack(track: Track, row: @Composable () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val mine = model.user?.let { it.id == track.uploadedBy.id || it.isAdmin } == true

    if (!mine || !model.swipeToDelete) {
        row()
        return
    }

    var confirming by remember(track.id) { mutableStateOf(false) }
    var error by remember(track.id) { mutableStateOf<String?>(null) }

    val offset = remember(track.id) { Animatable(0f) }
    val density = LocalDensity.current
    val threshold = with(density) { 160.dp.toPx() }
    val maxReveal = with(density) { 200.dp.toPx() }
    val revealed = offset.value < 0f

    Box(Modifier.fillMaxWidth()) {
        if (revealed) {
            Box(
                Modifier
                    .matchParentSize()
                    .background(MaterialTheme.colorScheme.errorContainer)
                    .padding(horizontal = 20.dp),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(
                    Icons.Filled.Delete,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onErrorContainer,
                )
            }
        }
        Box(
            Modifier
                .offset { IntOffset(offset.value.roundToInt(), 0) }
                .background(MaterialTheme.colorScheme.background)
                .pointerInput(track.id) {
                    detectHorizontalDragGestures(
                        onDragEnd = {
                            if (offset.value < -threshold) confirming = true
                            scope.launch { offset.animateTo(0f, spring(stiffness = Spring.StiffnessMedium)) }
                        },
                        onDragCancel = { scope.launch { offset.animateTo(0f) } },
                        onHorizontalDrag = { change, dx ->
                            change.consume()
                            scope.launch { offset.snapTo((offset.value + dx).coerceIn(-maxReveal, 0f)) }
                        },
                    )
                },
        ) { row() }
    }

    if (confirming) {
        DeleteTrackDialog(
            track = track,
            onDismiss = { confirming = false },
            onConfirm = { keep ->
                confirming = false
                scope.launch {
                    try {
                        model.deleteTrack(track, keep)
                    } catch (e: Exception) {
                        error = friendlyError(e)
                    }
                }
            },
        )
    }

    error?.let { message ->
        AlertDialog(
            onDismissRequest = { error = null },
            title = { Text("Couldn't delete track") },
            text = { Text(message) },
            confirmButton = { TextButton(onClick = { error = null }) { Text("OK") } },
        )
    }
}