package com.music.app.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.ui.formatBytes

@Composable
fun DeleteTrackDialog(track: Track, onDismiss: () -> Unit, onConfirm: (keep: Track?) -> Unit) {
    val model = LocalAppModel.current
    var copies by remember(track.id) { mutableStateOf<List<Track>?>(null) }
    var into by remember(track.id) { mutableStateOf<String?>(null) }

    LaunchedEffect(track.id) {
        val found = runCatching { model.trackCopies(track) }.getOrDefault(emptyList())
        copies = found
        into = found.firstOrNull()?.id
    }

    val found = copies
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Delete this track?") },
        text = {
            Column(
                Modifier.heightIn(max = 420.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text("“${track.title}” will be removed for everyone. This cannot be undone.")
                when {
                    found == null -> Row(Modifier.padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                    }
                    found.isNotEmpty() -> {
                        Text(
                            "Put another copy in its place in playlists and favorites:",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 12.dp, bottom = 2.dp),
                        )
                        found.forEach { copy ->
                            CopyOption(selected = into == copy.id, onSelect = { into = copy.id }) {
                                CoverImage(copy, size = 36.dp)
                                Column(Modifier.weight(1f).padding(start = 10.dp)) {
                                    Text(copy.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    Text(
                                        describeCopy(copy),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                        }
                        CopyOption(selected = into == null, onSelect = { into = null }) {
                            Text("None, just remove it")
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = found != null,
                onClick = { onConfirm(found?.firstOrNull { it.id == into }) },
            ) { Text("Delete", color = MaterialTheme.colorScheme.error) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun CopyOption(selected: Boolean, onSelect: () -> Unit, content: @Composable () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .clickable(onClick = onSelect)
            .padding(end = 8.dp, top = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = onSelect)
        content()
    }
}

fun describeCopy(t: Track): String = buildList {
    add(formatAudioLabel(t.mimeType))
    if (t.fileSize > 0) add(formatBytes(t.fileSize))
    add(
        when (t.source) {
            "youtube" -> "from YouTube"
            "ncs" -> "from NCS"
            "soundcloud" -> "from SoundCloud"
            "deezer" -> "from Deezer"
            else -> "uploaded"
        },
    )
    if (t.uploadedBy.username.isNotBlank()) add("by ${t.uploadedBy.username}")
    add("${t.plays} ${if (t.plays == 1) "play" else "plays"}")
}.joinToString(" • ")

private fun formatAudioLabel(mime: String): String {
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
