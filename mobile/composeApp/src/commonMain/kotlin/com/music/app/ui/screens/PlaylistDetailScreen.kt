package com.music.app.ui.screens

import com.music.app.ui.components.playlistAddMessage
import com.music.app.ui.components.CoverImage
import com.music.app.api.Track
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material.icons.filled.Search
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Link
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.DeepLinks
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.plainTextClipEntry
import com.music.app.api.PlaylistDetail
import com.music.app.api.friendlyError
import com.music.app.api.toPlaylist
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch

@Composable
fun PlaylistDetailScreen(screen: Screen.PlaylistDetail) {
    val model = LocalAppModel.current
    LaunchedEffect(screen.id) { model.loadPlaylist(screen.id) }

    var showAddDialog by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    var actionBusy by remember { mutableStateOf(false) }
    var actionError by remember { mutableStateOf<String?>(null) }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 8.dp, top = 8.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Column(Modifier.weight(1f)) {
                Text(
                    model.playlistDetail?.name ?: "Playlist",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val detail = model.playlistDetail
                if (detail != null) {
                    Text(
                        (if (detail.isMine) "" else "Shared by ${detail.owner.username} · ") +
                            "${detail.trackCount} tracks · ${formatTotalDuration(detail.totalDuration)}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            val header = model.playlistDetail
            if (header != null && header.isPublic) {
                SharePlaylistButton(header)
            }
            if (header != null) {
                PlaylistDownloadButton(header)
            }
            if (header != null && !header.isMine) {
                IconButton(
                    enabled = !actionBusy,
                    onClick = {
                        actionBusy = true
                        scope.launch {
                            try {
                                actionError = null
                                model.setPlaylistPinned(header.id, !header.isPinned)
                            } catch (e: Exception) {
                                actionError = friendlyError(e)
                            } finally {
                                actionBusy = false
                            }
                        }
                    },
                ) {
                    if (actionBusy) {
                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                    } else {
                        Icon(
                            Icons.Filled.PushPin,
                            contentDescription = if (header.isPinned) "Unpin playlist" else "Pin playlist",
                            tint = if (header.isPinned) {
                                Emerald
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
                            },
                        )
                    }
                }
            } else {
                if (header != null) {
                    IconButton(
                        enabled = !actionBusy,
                        onClick = {
                            actionBusy = true
                            scope.launch {
                                try {
                                    actionError = null
                                    model.setPlaylistPublic(header.id, !header.isPublic)
                                } catch (e: Exception) {
                                    actionError = friendlyError(e)
                                } finally {
                                    actionBusy = false
                                }
                            }
                        },
                    ) {
                        Icon(
                            if (header.isPublic) Icons.Filled.Public else Icons.Filled.Lock,
                            contentDescription = if (header.isPublic) "Make private" else "Make public",
                            tint = if (header.isPublic) Emerald else MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                IconButton(onClick = { showAddDialog = true }) {
                    Icon(Icons.Filled.Add, contentDescription = "Add songs")
                }
            }
        }

        actionError?.let { message ->
            Text(
                message,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }

        val detail = model.playlistDetail
        when {
            model.playlistDetailLoading && detail == null -> LoadingBox()
            model.playlistDetailError != null && detail == null ->
                ErrorState(model.playlistDetailError ?: "Could not load playlist") {
                    model.loadPlaylist(screen.id)
                }
            detail == null || detail.tracks.isEmpty() -> Column(
                Modifier.fillMaxWidth(),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                EmptyState(Icons.AutoMirrored.Filled.QueueMusic, "Empty playlist")
                if (detail?.isMine == true) {
                    Button(onClick = { showAddDialog = true }) {
                        Icon(Icons.Filled.Add, contentDescription = null)
                        Spacer(Modifier.size(4.dp))
                        Text("Add songs")
                    }
                }
            }
            else -> {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 8.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Button(
                        onClick = { model.playAll(detail.tracks, playContext = "playlist:${detail.id}") },
                        modifier = Modifier.weight(1f),
                    ) {
                        Icon(Icons.Filled.PlayArrow, contentDescription = null)
                        Spacer(Modifier.size(4.dp))
                        Text("Play all")
                    }
                    if (model.listen.session?.canControl == true) {
                        OutlinedButton(
                            onClick = { model.listen.add(detail.tracks, "end") },
                            modifier = Modifier.weight(1f),
                        ) {
                            Icon(Icons.AutoMirrored.Filled.PlaylistAdd, contentDescription = null)
                            Spacer(Modifier.size(4.dp))
                            Text("Add to queue")
                        }
                    }
                }
                RefreshableContent(
                    isRefreshing = model.playlistDetailLoading,
                    onRefresh = { model.loadPlaylist(detail.id) },
                ) {
                LazyColumn(Modifier.fillMaxSize()) {
                    items(detail.tracks, key = { it.id }) { track ->
                        TrackRow(
                            track = track,
                            context = detail.tracks,
                            playContext = "playlist:${detail.id}",
                            onRemove = if (detail.isMine) {
                                { id: String -> model.removeTrackFromPlaylist(screen.id, id) }
                            } else {
                                null
                            },
                            trailing = {
                                if (detail.isMine && !model.selecting) {
                                    IconButton(onClick = {
                                        model.removeTrackFromPlaylist(screen.id, track.id)
                                    }) {
                                        Icon(
                                            Icons.Filled.Close,
                                            contentDescription = "Remove from playlist",
                                            tint = Zinc500,
                                        )
                                    }
                                }
                            },
                        )
                    }
                }
                }
            }
        }
    }

    if (showAddDialog) {
        AddTracksDialog(
            playlistId = screen.id,
            onDismiss = { showAddDialog = false },
        )
    }
}

@Composable
private fun AddTracksDialog(playlistId: String, onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf<Set<String>>(emptySet()) }
    var query by remember { mutableStateOf("") }

    LaunchedEffect(Unit) { if (model.tracks == null) model.loadLibrary() }

    val tracks = model.tracks
    val detail = model.playlistDetail
    val already = remember(detail) { detail?.tracks?.map { it.id }?.toSet() ?: emptySet() }
    val words = remember(query) { query.lowercase().split(Regex("\\s+")).filter { it.isNotBlank() } }
    val results = remember(tracks, words) {
        val all = tracks.orEmpty()
        if (words.isEmpty()) {
            all.sortedByDescending { it.createdAt }.take(RECENT_TRACKS)
        } else {
            all.asSequence()
                .filter { t ->
                    val hay = "${t.title} ${t.artist} ${t.album}".lowercase()
                    words.all { it in hay }
                }
                .take(MAX_RESULTS)
                .toList()
        }
    }
    val addable = results.filter { it.id !in already && it.id !in busy }

    fun add(list: List<Track>) {
        if (list.isEmpty()) return
        busy = busy + list.map { it.id }
        scope.launch {
            error = null
            try {
                val name = detail?.name ?: "playlist"
                val result = model.addTracksToPlaylist(playlistId, list)
                if (list.size > 1) model.notify(playlistAddMessage(result, list.size, name))
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = busy - list.map { it.id }.toSet()
            }
        }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add songs") },
        text = {
            Column {
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    singleLine = true,
                    placeholder = { Text("Search your library") },
                    leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(
                    Modifier.fillMaxWidth().padding(top = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        when {
                            words.isEmpty() -> "Recently added"
                            results.isEmpty() -> "No matches"
                            results.size >= MAX_RESULTS -> "First $MAX_RESULTS matches"
                            results.size == 1 -> "1 match"
                            else -> "${results.size} matches"
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(1f),
                    )
                    if (words.isNotEmpty() && addable.size > 1) {
                        TextButton(onClick = { add(addable) }) { Text("Add all ${addable.size}") }
                    }
                }
                error?.let {
                    Text(
                        it,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Spacer(Modifier.height(8.dp))
                }
                when {
                    tracks == null -> LoadingBox(Modifier.heightIn(max = 160.dp))
                    else -> LazyColumn(
                        Modifier
                            .heightIn(max = 360.dp)
                            .fillMaxWidth(),
                    ) {
                        items(results, key = { it.id }) { track ->
                            val inside = track.id in already
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .clickable(enabled = !inside && track.id !in busy) { add(listOf(track)) }
                                    .padding(vertical = 6.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                CoverImage(track, 40.dp)
                                Column(Modifier.weight(1f).padding(horizontal = 10.dp)) {
                                    Text(
                                        track.title,
                                        style = MaterialTheme.typography.bodyMedium,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                    Text(
                                        track.artist.ifBlank { "Unknown artist" },
                                        style = MaterialTheme.typography.bodySmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                                when {
                                    track.id in busy -> CircularProgressIndicator(
                                        Modifier.size(18.dp),
                                        strokeWidth = 2.dp,
                                    )
                                    inside -> Icon(
                                        Icons.Filled.Check,
                                        contentDescription = "Already in playlist",
                                        tint = Emerald,
                                    )
                                    else -> Icon(Icons.Filled.Add, contentDescription = "Add", tint = Zinc500)
                                }
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("Done") }
        },
    )
}

private const val RECENT_TRACKS = 30
private const val MAX_RESULTS = 60


@Composable
private fun PlaylistDownloadButton(detail: PlaylistDetail) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val downloaded = model.downloads.isPlaylistDownloaded(detail.id)

    var busy by remember { mutableStateOf(false) }
    var done by remember { mutableStateOf(0) }
    var total by remember { mutableStateOf(0) }
    var confirmRemove by remember { mutableStateOf(false) }

    val empty = detail.tracks.isEmpty()

    IconButton(
        enabled = !busy && !empty,
        onClick = {
            if (downloaded) {
                confirmRemove = true
            } else {
                busy = true
                scope.launch {
                    try {
                        model.downloads.downloadPlaylist(detail.toPlaylist(), detail.tracks) { d, t ->
                            done = d
                            total = t
                        }
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
                contentDescription = if (model.downloads.isKeptOffline(detail.id)) {
                    "Kept offline — tap for options"
                } else {
                    "Downloaded — tap for options"
                },
                tint = Emerald,
            )
            else -> Icon(
                Icons.Filled.Download,
                contentDescription = if (empty) {
                    "This playlist is empty"
                } else {
                    "Download playlist for offline use"
                },
                tint = MaterialTheme.colorScheme.onSurfaceVariant
                    .copy(alpha = if (empty) 0.3f else 1f),
            )
        }
    }

    if (confirmRemove) {
        val kept = model.downloads.isKeptOffline(detail.id)
        AlertDialog(
            onDismissRequest = { confirmRemove = false },
            title = { Text("Downloaded playlist") },
            text = if (kept) ({ Text("New tracks download automatically.") }) else null,
            confirmButton = {
                Column(horizontalAlignment = Alignment.End) {
                    TextButton(onClick = {
                        scope.launch {
                            model.downloads.setKeptOffline(detail.toPlaylist(), detail.tracks, keep = !kept)
                        }
                        confirmRemove = false
                    }) { Text(if (kept) "Stop keeping offline" else "Keep offline") }
                    TextButton(onClick = {
                        model.downloads.removePlaylist(detail.id)
                        confirmRemove = false
                    }) { Text("Remove download", color = MaterialTheme.colorScheme.error) }
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmRemove = false }) { Text("Cancel") }
            },
        )
    }
}


@Composable
private fun SharePlaylistButton(detail: PlaylistDetail) {
    val model = LocalAppModel.current
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    var copied by remember(detail.id) { mutableStateOf(false) }

    IconButton(
        onClick = {
            scope.launch {
                clipboard.setClipEntry(
                    plainTextClipEntry(DeepLinks.shareUrl(model.api.baseUrl, detail.id)),
                )
                copied = true
            }
        },
    ) {
        Icon(
            if (copied) Icons.Filled.Check else Icons.Filled.Link,
            contentDescription = if (copied) "Link copied" else "Copy link to playlist",
            tint = if (copied) Emerald else MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
