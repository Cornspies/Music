package com.music.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.Track
import com.music.app.api.friendlyError
import kotlinx.coroutines.launch
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
)

/** Library: all tracks with server-side sorting and an entry point to upload. */
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
                                model.loadLibrary(value)
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

/**
 * Swipe a row far enough and a confirmation dialog appears — the standard
 * Android pattern. The row always springs back; nothing is removed until the
 * dialog is confirmed, so a stray swipe costs nothing.
 *
 * Only the uploader can delete a track (the server answers 403 otherwise), so
 * everyone else's rows are not swipeable at all rather than offering an action
 * that would fail.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeToDeleteTrack(track: Track, row: @Composable () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    // The uploader may edit or delete; so may an admin (the server enforces both).
    val mine = model.user?.let { it.id == track.uploadedBy.id || it.isAdmin } == true

    if (!mine) {
        row()
        return
    }

    var confirming by remember(track.id) { mutableStateOf(false) }
    var error by remember(track.id) { mutableStateOf<String?>(null) }

    val state = rememberSwipeToDismissBoxState(
        confirmValueChange = { value ->
            if (value != SwipeToDismissBoxValue.Settled) confirming = true
            // Always refuse the dismissal itself: the row stays put and the
            // dialog decides. Letting it dismiss first would animate the row
            // away before the user has agreed to anything.
            false
        },
    )

    val swiping = state.dismissDirection != SwipeToDismissBoxValue.Settled

    SwipeToDismissBox(
        state = state,
        // Delete is a right-to-left swipe only. A left-to-right drag is the
        // gesture people make to scroll or go back, so offering the same
        // destructive action there invited accidental confirmations.
        enableDismissFromStartToEnd = false,
        backgroundContent = {
            // Only drawn while a swipe is actually in progress. The box sits
            // behind the row at all times, so painting it unconditionally left
            // a red stripe and a bin icon visible on every row.
            if (swiping) {
                Box(
                    Modifier
                        .fillMaxSize()
                        .background(MaterialTheme.colorScheme.errorContainer)
                        .padding(horizontal = 20.dp),
                    // The row slides left, so the bin is revealed on the right.
                    contentAlignment = Alignment.CenterEnd,
                ) {
                    Icon(
                        Icons.Filled.Delete,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onErrorContainer,
                    )
                }
            }
        },
        content = {
            // TrackRow itself is transparent, so the row needs an opaque
            // backdrop of its own or the background shows through it as it
            // slides rather than being revealed from behind.
            Box(Modifier.background(MaterialTheme.colorScheme.background)) { row() }
        },
    )

    if (confirming) {
        AlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text("Delete this track?") },
            text = {
                Text("“${track.title}” will be removed for everyone. This cannot be undone.")
            },
            confirmButton = {
                TextButton(onClick = {
                    confirming = false
                    scope.launch {
                        try {
                            model.deleteTrack(track)
                        } catch (e: Exception) {
                            error = friendlyError(e)
                        }
                    }
                }) {
                    Text("Delete", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirming = false }) { Text("Cancel") }
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