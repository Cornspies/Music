package com.music.app.ui.components

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.api.friendlyError
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch

/**
 * "Add to playlist" for the currently playing track.
 *
 * The sheet stays open after each tap so one track can go into several
 * playlists in a row, each showing a tick once it's in. Only your own
 * playlists are listed — playlists pinned from other people are read-only.
 */
@Composable
fun AddToPlaylistButton(track: Track, modifier: Modifier = Modifier) {
    val model = LocalAppModel.current
    var open by remember { mutableStateOf(false) }

    IconButton(modifier = modifier, onClick = { open = true }) {
        Icon(
            Icons.AutoMirrored.Filled.PlaylistAdd,
            contentDescription = "Add ${track.title} to a playlist",
            tint = Zinc500,
        )
    }

    if (open) {
        AddToPlaylistSheet(track = track, onDismiss = { open = false })
    }

    // Playlists may not have been fetched yet on a cold start.
    LaunchedEffect(open) {
        if (open && model.playlists == null) model.loadPlaylists()
    }
}

@Composable
private fun AddToPlaylistSheet(track: Track, onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()

    val own = (model.playlists ?: emptyList()).filter { it.isMine }
    var pending by remember { mutableStateOf<String?>(null) }
    var added by remember { mutableStateOf<Set<String>>(emptySet()) }
    var error by remember { mutableStateOf<String?>(null) }

    AlertDialog(
        onDismissRequest = { if (pending == null) onDismiss() },
        title = {
            Column {
                Text("Add to playlist", fontWeight = FontWeight.Bold)
                Text(
                    track.title,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        },
        text = {
            Column(Modifier.fillMaxWidth()) {
                error?.let { message ->
                    Text(
                        message,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(bottom = 8.dp),
                    )
                }
                when {
                    model.playlists == null -> LoadingBox(Modifier.heightIn(max = 120.dp))
                    own.isEmpty() -> Text(
                        "You don't have any playlists yet. Create one.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    else -> LazyColumn(Modifier.heightIn(max = 320.dp)) {
                        items(own, key = { it.id }) { playlist ->
                            val isAdded = added.contains(playlist.id)
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .padding(vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                when {
                                    pending == playlist.id -> CircularProgressIndicator(
                                        Modifier.size(18.dp),
                                        strokeWidth = 2.dp,
                                    )
                                    isAdded -> Icon(Icons.Filled.Check, contentDescription = null, tint = Emerald)
                                    else -> Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = null, tint = Zinc500)
                                }
                                Text(
                                    playlist.name,
                                    modifier = Modifier
                                        .weight(1f)
                                        .padding(start = 12.dp),
                                    color = if (isAdded) Emerald else MaterialTheme.colorScheme.onSurface,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                TextButton(
                                    enabled = pending == null && !isAdded,
                                    onClick = {
                                        pending = playlist.id
                                        scope.launch {
                                            try {
                                                error = null
                                                model.addTrackToPlaylist(playlist.id, track.id)
                                                added = added + playlist.id
                                            } catch (e: Exception) {
                                                error = friendlyError(e)
                                            } finally {
                                                pending = null
                                            }
                                        }
                                    },
                                ) {
                                    Text(if (isAdded) "Added" else "Add")
                                }
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss, enabled = pending == null) { Text("Done") }
        },
    )
}
