package com.music.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.DuplicateGroup
import com.music.app.api.Track
import com.music.app.api.friendlyError
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatBytes
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc800
import kotlinx.coroutines.launch

@Composable
fun DuplicatesScreen() {
    val model = LocalAppModel.current
    var result by remember { mutableStateOf<Pair<List<DuplicateGroup>?, String?>?>(null) }
    var reloadTick by remember { mutableStateOf(0) }

    LaunchedEffect(reloadTick) {
        result = try {
            model.api.libraryDuplicates() to null
        } catch (e: Exception) {
            null to friendlyError(e)
        }
    }

    val alive = model.tracks?.map { it.id }?.toSet()
    val groups = result?.first?.map { g ->
        if (alive == null) g else g.copy(tracks = g.tracks.filter { it.id in alive })
    }?.filter { it.tracks.size > 1 }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 8.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Icon(Icons.Filled.ContentCopy, contentDescription = null, tint = Emerald)
            Spacer(Modifier.width(8.dp))
            Text(
                "Duplicates",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }
        if (groups != null && groups.isNotEmpty()) {
            val extra = groups.sumOf { it.tracks.size - 1 }
            Text(
                "${groups.size} ${if (groups.size == 1) "song" else "songs"} with extra copies • $extra to look at",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        Spacer(Modifier.height(8.dp))

        val error = result?.second
        when {
            groups == null && error != null -> ErrorState(error, onRetry = { reloadTick++ })
            groups == null -> LoadingBox()
            groups.isEmpty() -> EmptyState(icon = Icons.Filled.ContentCopy, title = "No duplicates found")
            else -> RefreshableContent(isRefreshing = false, onRefresh = { reloadTick++ }) {
                LazyColumn(
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                    contentPadding = PaddingValues(start = 12.dp, end = 12.dp, bottom = 24.dp),
                ) {
                    items(groups, key = { g -> g.tracks.joinToString("+") { it.id } }) { g ->
                        DuplicateGroupCard(g)
                    }
                }
            }
        }
    }
}

@Composable
private fun DuplicateGroupCard(group: DuplicateGroup) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var confirming by remember { mutableStateOf<Track?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val first = group.tracks.first()

    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(Zinc800.copy(alpha = 0.35f))
            .padding(vertical = 6.dp),
    ) {
        Text(
            buildString {
                append(first.title)
                if (first.artist.isNotBlank()) append(" • ").append(first.artist)
                append(" • ").append(group.reason)
            },
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 2.dp),
        )
        group.tracks.forEach { track ->
            val canDelete = model.user?.let { it.isAdmin || it.id == track.uploadedBy.id } == true
            Column {
                TrackRow(track = track, context = group.tracks, showActions = false) {
                    if (canDelete) {
                        IconButton(onClick = { confirming = track }) {
                            Icon(Icons.Filled.Delete, contentDescription = "Delete this copy", tint = MaterialTheme.colorScheme.error)
                        }
                    }
                }
                Text(
                    describe(track),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    modifier = Modifier.padding(start = 72.dp, end = 12.dp, bottom = 4.dp),
                )
            }
        }
        error?.let {
            Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 12.dp))
        }
    }

    confirming?.let { track ->
        AlertDialog(
            onDismissRequest = { confirming = null },
            title = { Text("Delete this copy?") },
            text = { Text("“${track.title}” (${describe(track)}) will be removed for everyone. This cannot be undone.") },
            confirmButton = {
                TextButton(onClick = {
                    confirming = null
                    scope.launch {
                        try {
                            model.deleteTrack(track)
                        } catch (e: Exception) {
                            error = friendlyError(e)
                        }
                    }
                }) { Text("Delete", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { confirming = null }) { Text("Cancel") } },
        )
    }
}

private fun describe(t: Track): String = buildList {
    add(formatLabel(t.mimeType))
    if (t.fileSize > 0) add(formatBytes(t.fileSize))
    add(
        when (t.source) {
            "youtube" -> "from YouTube"
            "ncs" -> "from NCS"
            else -> "uploaded"
        },
    )
    if (t.uploadedBy.username.isNotBlank()) add("by ${t.uploadedBy.username}")
    add("${t.plays} ${if (t.plays == 1) "play" else "plays"}")
}.joinToString(" • ")

private fun formatLabel(mime: String): String {
    val m = mime.lowercase()
    return when {
        "mpeg" in m -> "MP3"
        "flac" in m -> "FLAC"
        "wav" in m -> "WAV"
        "ogg" in m -> "OGG"
        "mp4" in m || "m4a" in m -> "M4A"
        "aac" in m -> "AAC"
        m.isBlank() -> "audio"
        else -> m
    }
}
