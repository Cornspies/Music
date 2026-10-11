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
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import com.music.app.api.PlaylistAddResult
import com.music.app.api.Track
import com.music.app.api.friendlyError
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch

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
        AddToPlaylistDialog(tracks = listOf(track), onDismiss = { open = false })
    }

    LaunchedEffect(open) {
        if (open && model.playlists == null) model.loadPlaylists()
    }
}

fun playlistAddMessage(result: PlaylistAddResult, count: Int, name: String): String = when {
    result.added == 0 && count == 1 -> "Already in $name"
    result.added == 0 -> "All of these are already in $name"
    else -> "Added ${result.added} ${if (result.added == 1) "song" else "songs"} to $name" +
        if (result.skipped > 0) " · ${result.skipped} already there" else ""
}

@Composable
fun AddToPlaylistDialog(tracks: List<Track>, onDismiss: () -> Unit, onAdded: (() -> Unit)? = null) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) { if (model.playlists == null) model.loadPlaylists() }

    val own = (model.playlists ?: emptyList()).filter { it.isMine }
    var pending by remember { mutableStateOf<String?>(null) }
    var added by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    var error by remember { mutableStateOf<String?>(null) }
    var creating by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }

    fun addTo(id: String, name: String, create: Boolean = false) {
        pending = id
        scope.launch {
            try {
                error = null
                val target = if (create) model.createPlaylist(name) else null
                val result = model.addTracksToPlaylist(target?.id ?: id, tracks)
                val message = playlistAddMessage(result, tracks.size, target?.name ?: name)
                if (onAdded != null) {
                    model.notify(message)
                    onAdded()
                } else {
                    added = added + ((target?.id ?: id) to if (result.added == 0) "Already there" else "Added")
                    creating = false
                    newName = ""
                }
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                pending = null
            }
        }
    }

    AlertDialog(
        onDismissRequest = { if (pending == null) onDismiss() },
        title = {
            Column {
                Text("Add to playlist", fontWeight = FontWeight.Bold)
                Text(
                    if (tracks.size == 1) tracks[0].title else "${tracks.size} songs",
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
                    own.isEmpty() && !creating -> Text(
                        "No playlists yet.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    else -> LazyColumn(Modifier.heightIn(max = 320.dp)) {
                        items(own, key = { it.id }) { playlist ->
                            val state = added[playlist.id]
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .padding(vertical = 4.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                when {
                                    pending == playlist.id -> CircularProgressIndicator(
                                        Modifier.size(18.dp),
                                        strokeWidth = 2.dp,
                                    )
                                    state != null -> Icon(Icons.Filled.Check, contentDescription = null, tint = Emerald)
                                    else -> Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = null, tint = Zinc500)
                                }
                                Text(
                                    playlist.name,
                                    modifier = Modifier
                                        .weight(1f)
                                        .padding(start = 12.dp),
                                    color = if (state != null) Emerald else MaterialTheme.colorScheme.onSurface,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                TextButton(
                                    enabled = pending == null && state == null,
                                    onClick = { addTo(playlist.id, playlist.name) },
                                ) {
                                    Text(state ?: "Add")
                                }
                            }
                        }
                    }
                }
                if (creating) {
                    Row(
                        Modifier.fillMaxWidth().padding(top = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        OutlinedTextField(
                            value = newName,
                            onValueChange = { newName = it.take(80) },
                            singleLine = true,
                            placeholder = { Text("Playlist name") },
                            modifier = Modifier.weight(1f),
                        )
                        TextButton(
                            enabled = pending == null && newName.isNotBlank(),
                            onClick = { addTo("new", newName.trim(), create = true) },
                        ) {
                            if (pending == "new") {
                                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            } else {
                                Text("Create")
                            }
                        }
                    }
                } else if (model.playlists != null) {
                    TextButton(onClick = { creating = true }, enabled = pending == null) {
                        Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                        Text("New playlist", modifier = Modifier.padding(start = 6.dp))
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss, enabled = pending == null) { Text("Done") }
        },
    )
}
