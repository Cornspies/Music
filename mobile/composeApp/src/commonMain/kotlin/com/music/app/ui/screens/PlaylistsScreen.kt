package com.music.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.PushPin
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
import com.music.app.Screen
import com.music.app.api.Playlist
import com.music.app.api.friendlyError
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.UserAvatar
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.EmeraldDark
import kotlinx.coroutines.launch

/** Playlists: list, create dialog, open detail, delete. */
@Composable
fun PlaylistsScreen() {
    val model = LocalAppModel.current
    LaunchedEffect(Unit) { model.loadPlaylists() }

    var showCreate by remember { mutableStateOf(false) }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    "Playlists",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "${model.playlists?.size ?: 0} playlists",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            IconButton(onClick = { showCreate = true }) {
                Icon(Icons.Filled.Add, contentDescription = "New playlist")
            }
        }

        val playlists = model.playlists
        when {
            model.playlistsLoading && playlists == null -> LoadingBox()
            model.playlistsError != null && playlists == null ->
                ErrorState(model.playlistsError ?: "Could not load playlists") { model.loadPlaylists() }
            playlists == null || playlists.isEmpty() -> EmptyState(
                Icons.AutoMirrored.Filled.QueueMusic,
                "No playlists",
                subtitle = "Create one to group tracks for any mood or road trip.",
            )
            else -> RefreshableContent(
                isRefreshing = model.playlistsLoading,
                onRefresh = { model.loadPlaylists() },
            ) {
            LazyColumn(Modifier.fillMaxSize()) {
                items(playlists, key = { it.id }) { playlist ->
                    PlaylistRow(playlist)
                }
                // Public playlists you haven't pinned yet — pinning moves them
                // into the list above.
                val available = model.publicPlaylists.filter { p -> playlists.none { it.id == p.id } }
                if (available.isNotEmpty()) {
                    item(key = "shared-header") {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
                            Text(
                                "Shared with you",
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.Bold,
                            )
                            Text(
                                "Playlists other people made public. Pin one to keep it here.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    items(available, key = { it.id }) { playlist ->
                        PlaylistRow(playlist)
                    }
                }
            }
            }
        }
    }

    if (showCreate) {
        CreatePlaylistDialog(
            onDismiss = { showCreate = false },
        )
    }
}

@Composable
private fun PlaylistRow(playlist: Playlist) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var confirmDelete by remember { mutableStateOf(false) }
    var pinBusy by remember { mutableStateOf(false) }
    var pinError by remember { mutableStateOf<String?>(null) }

    Row(
        Modifier
            .fillMaxWidth()
            .clickable { model.push(Screen.PlaylistDetail(playlist.id)) }
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
            Box(
                Modifier
                    .size(48.dp)
                    .background(EmeraldDark.copy(alpha = 0.25f), RoundedCornerShape(10.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = null, tint = Emerald)
            }
            // Someone else's playlist: their picture on the corner makes
            // "mine" vs "theirs" obvious without reading the subtitle.
            if (!playlist.isMine && playlist.owner.id.isNotEmpty()) {
                Box(Modifier.align(Alignment.BottomEnd)) {
                    UserAvatar(
                        userId = playlist.owner.id,
                        username = playlist.owner.username,
                        hasAvatar = playlist.owner.hasAvatar,
                        size = 18.dp,
                        label = "Shared by ${playlist.owner.username}",
                    )
                }
            }
        }
        Column(
            Modifier
                .weight(1f)
                .padding(horizontal = 12.dp),
        ) {
            Text(
                playlist.name,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                (if (playlist.isMine) "" else playlist.owner.username.ifBlank { "shared" } + " · ") +
                    "${playlist.trackCount} tracks · ${formatTotalDuration(playlist.totalDuration)}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            pinError?.let { message ->
                Text(
                    message,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        PlaylistRowDownloadButton(playlist)
        if (playlist.isMine) {
            IconButton(onClick = { confirmDelete = true }) {
                Icon(Icons.Filled.Delete, contentDescription = "Delete playlist")
            }
        } else {
            // Not yours: the useful action is keeping (or dropping) it here.
            IconButton(
                enabled = !pinBusy,
                onClick = {
                    pinBusy = true
                    scope.launch {
                        try {
                            pinError = null
                            model.setPlaylistPinned(playlist.id, !playlist.isPinned)
                        } catch (e: Exception) {
                            pinError = friendlyError(e)
                        } finally {
                            pinBusy = false
                        }
                    }
                },
            ) {
                if (pinBusy) {
                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                } else {
                    Icon(
                        Icons.Filled.PushPin,
                        contentDescription = if (playlist.isPinned) "Unpin playlist" else "Pin playlist",
                        tint = if (playlist.isPinned) {
                            Emerald
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
                        },
                    )
                }
            }
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Delete \"${playlist.name}\"?") },
            text = { Text("The playlist is removed. Your tracks stay in the library.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    model.deletePlaylist(playlist.id)
                }) { Text("Delete") }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = false }) { Text("Cancel") }
            },
        )
    }
}

@Composable
private fun CreatePlaylistDialog(onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text("New playlist") },
        text = {
            Column {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Name") },
                    singleLine = true,
                    isError = error != null,
                    supportingText = error?.let { { Text(it, color = MaterialTheme.colorScheme.error) } },
                )
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    scope.launch {
                        busy = true
                        error = null
                        try {
                            model.createPlaylist(name)
                            name = ""
                            onDismiss()
                        } catch (e: Exception) {
                            error = friendlyError(e)
                        } finally {
                            busy = false
                        }
                    }
                },
                enabled = !busy && name.isNotBlank(),
            ) {
                if (busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                    )
                } else {
                    Text("Create")
                }
            }
        },
        dismissButton = {
            TextButton(onClick = { if (!busy) onDismiss() }) { Text("Cancel") }
        },
    )
}


/**
 * Downloads a whole playlist straight from the overview.
 *
 * The overview only knows a playlist's summary, so the track list is fetched
 * when the download starts. Progress replaces the icon because a playlist takes
 * long enough that a static icon would look stuck.
 */
@Composable
private fun PlaylistRowDownloadButton(playlist: Playlist) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val downloaded = model.downloads.isPlaylistDownloaded(playlist.id)

    var busy by remember(playlist.id) { mutableStateOf(false) }
    var done by remember(playlist.id) { mutableStateOf(0) }
    var total by remember(playlist.id) { mutableStateOf(0) }
    var confirmRemove by remember(playlist.id) { mutableStateOf(false) }
    var error by remember(playlist.id) { mutableStateOf<String?>(null) }

    // An empty playlist has nothing to keep offline, so the control is
    // disabled rather than silently doing nothing when tapped.
    val empty = playlist.trackCount == 0

    IconButton(
        enabled = !busy && !empty,
        onClick = {
            if (downloaded) {
                confirmRemove = true
            } else {
                busy = true
                scope.launch {
                    try {
                        error = null
                        model.downloadPlaylistById(playlist) { d, t ->
                            done = d
                            total = t
                        }
                    } catch (e: Exception) {
                        error = friendlyError(e)
                    } finally {
                        busy = false
                    }
                }
            }
        },
    ) {
        when {
            busy && total > 0 -> CircularProgressIndicator(
                progress = { done.toFloat() / total.toFloat() },
                modifier = Modifier.size(20.dp),
                strokeWidth = 2.dp,
                color = Emerald,
            )
            busy -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp, color = Emerald)
            downloaded -> Icon(
                Icons.Filled.DownloadDone,
                contentDescription = "Downloaded — tap to remove",
                tint = Emerald,
            )
            else -> Icon(
                Icons.Filled.Download,
                contentDescription = if (empty) {
                    "Nothing to download — this playlist is empty"
                } else {
                    "Download playlist for offline"
                },
                tint = MaterialTheme.colorScheme.onSurfaceVariant
                    .copy(alpha = if (empty) 0.3f else 0.7f),
            )
        }
    }

    error?.let { message ->
        AlertDialog(
            onDismissRequest = { error = null },
            title = { Text("Couldn't download playlist") },
            text = { Text(message) },
            confirmButton = { TextButton(onClick = { error = null }) { Text("OK") } },
        )
    }

    if (confirmRemove) {
        AlertDialog(
            onDismissRequest = { confirmRemove = false },
            title = { Text("Remove downloaded playlist?") },
            text = {
                Text(
                    "The audio is deleted from this device, except for tracks another " +
                        "downloaded playlist still needs. The playlist itself is untouched.",
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    model.downloads.removePlaylist(playlist.id)
                    confirmRemove = false
                }) { Text("Remove") }
            },
            dismissButton = { TextButton(onClick = { confirmRemove = false }) { Text("Cancel") } },
        )
    }
}
