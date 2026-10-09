package com.music.app.ui.screens

import androidx.compose.foundation.layout.Column
import kotlin.math.roundToInt
import com.music.app.ui.components.CoverImage
import com.music.app.api.friendlyError
import com.music.app.api.DuplicateResponse
import com.music.app.api.DuplicateException
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.material3.TextButton
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CloudUpload
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
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
import com.music.app.PickedAudio
import com.music.app.canUploadFromDevice
import com.music.app.mimeTypeForFile
import com.music.app.openChannel
import com.music.app.rememberAudioPickerLauncher
import com.music.app.api.Track
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.MetadataReviewDialog
import com.music.app.ui.formatBytes
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.Emerald
import kotlinx.coroutines.launch

private enum class UploadStatus { PENDING, UPLOADING, DONE, REPLACED, FAILED, DUPLICATE, SKIPPED }

private data class UploadEntry(
    val file: PickedAudio,
    val status: UploadStatus,
    val error: String? = null,
    val duplicate: DuplicateResponse? = null,
)

@Composable
fun UploadScreen() {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()

    var entries by remember { mutableStateOf(listOf<UploadEntry>()) }
    var uploading by remember { mutableStateOf(false) }
    var uploadedTracks by remember { mutableStateOf(listOf<Track>()) }
    var reviewOpen by remember { mutableStateOf(false) }

    val pick = rememberAudioPickerLauncher { picked ->
        entries = picked.map { UploadEntry(it, UploadStatus.PENDING) }
        uploadedTracks = emptyList()
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 8.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Icon(Icons.Filled.CloudUpload, contentDescription = null, tint = Emerald)
            Spacer(Modifier.width(8.dp))
            Text(
                "Upload music",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }
        Spacer(Modifier.height(8.dp))

        Column(Modifier.padding(horizontal = 16.dp)) {

            if (canUploadFromDevice) {
                Button(onClick = pick, enabled = !uploading) {
                    Text(if (entries.isEmpty()) "Choose audio files" else "Choose different files")
                }
                Spacer(Modifier.height(16.dp))

                entries.forEachIndexed { index, entry ->
                    UploadRow(
                        entry,
                        canReplace = { track -> model.user?.let { it.id == track.uploadedBy.id || it.isAdmin } == true },
                        onReplace = { trackId ->
                            val pending = entry.duplicate?.pendingId ?: return@UploadRow
                            entries = entries.toMutableList().also { it[index] = entry.copy(status = UploadStatus.UPLOADING) }
                            scope.launch {
                                try {
                                    model.api.replaceWithPendingUpload(pending, trackId)
                                    entries = entries.toMutableList().also {
                                        it[index] = entry.copy(status = UploadStatus.REPLACED, duplicate = null)
                                    }
                                    model.loadLibrary()
                                } catch (e: Exception) {
                                    entries = entries.toMutableList().also {
                                        it[index] = entry.copy(status = UploadStatus.FAILED, error = friendlyError(e), duplicate = null)
                                    }
                                }
                            }
                        },
                        onAddAnyway = {
                            val pending = entry.duplicate?.pendingId ?: return@UploadRow
                            entries = entries.toMutableList().also { it[index] = entry.copy(status = UploadStatus.UPLOADING) }
                            scope.launch {
                                try {
                                    val track = model.api.confirmPendingUpload(pending)
                                    entries = entries.toMutableList().also { it[index] = entry.copy(status = UploadStatus.DONE, duplicate = null) }
                                    uploadedTracks = uploadedTracks + track
                                    model.loadLibrary()
                                } catch (e: Exception) {
                                    entries = entries.toMutableList().also {
                                        it[index] = entry.copy(status = UploadStatus.FAILED, error = friendlyError(e), duplicate = null)
                                    }
                                }
                            }
                        },
                        onSkip = {
                            entry.duplicate?.pendingId?.let { id -> scope.launch { runCatching { model.api.discardPendingUpload(id) } } }
                            entries = entries.toMutableList().also { it[index] = entry.copy(status = UploadStatus.SKIPPED, duplicate = null) }
                        },
                    )
                    if (index != entries.lastIndex) Spacer(Modifier.height(8.dp))
                }

                if (entries.isNotEmpty()) {
                    Spacer(Modifier.height(16.dp))
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Button(
                            onClick = {
                                if (uploading) return@Button
                                scope.launch {
                                    uploading = true
                                    uploadedTracks = emptyList()
                                    entries = entries.map { it.copy(status = UploadStatus.UPLOADING) }
                                    val succeeded = mutableListOf<Track>()
                                    for ((i, entry) in entries.withIndex()) {
                                        try {
                                            val track = model.api.uploadTrack(
                                                fileName = entry.file.name,
                                                mimeType = mimeTypeForFile(entry.file.name),
                                                sizeBytes = entry.file.sizeBytes,
                                                content = { entry.file.openChannel() },
                                                durationSec = entry.file.durationSec,
                                            )
                                            succeeded.add(track)
                                            entries = entries.toMutableList().also {
                                                it[i] = entry.copy(status = UploadStatus.DONE)
                                            }
                                        } catch (e: DuplicateException) {
                                            entries = entries.toMutableList().also {
                                                it[i] = entry.copy(status = UploadStatus.DUPLICATE, duplicate = e.response)
                                            }
                                        } catch (e: Exception) {
                                            entries = entries.toMutableList().also {
                                                it[i] = entry.copy(
                                                    status = UploadStatus.FAILED,
                                                    error = e.message ?: "Upload failed",
                                                )
                                            }
                                        }
                                    }
                                    uploading = false
                                    uploadedTracks = succeeded
                                    model.loadLibrary()
                                    model.loadStats()
                                }
                            },
                            enabled = !uploading,
                        ) {
                            Text("Upload ${entries.size} file(s)")
                        }
                        if (uploading) {
                            Spacer(Modifier.padding(start = 12.dp))
                            CircularProgressIndicator(Modifier.height(20.dp))
                        }
                    }

                    if (!uploading && uploadedTracks.isNotEmpty()) {
                        Spacer(Modifier.height(12.dp))
                        OutlinedButton(onClick = { reviewOpen = true }) {
                            Icon(
                                Icons.Filled.AutoAwesome,
                                contentDescription = null,
                                tint = Emerald,
                                modifier = Modifier.height(18.dp),
                            )
                            Spacer(Modifier.padding(start = 8.dp))
                            Text("Check track details (${uploadedTracks.size})")
                        }
                    }
                }
            } else {
                EmptyState(
                    icon = Icons.Filled.CloudUpload,
                    title = "Upload from the web app",
                )
            }

            Spacer(Modifier.height(24.dp))
        }
    }

    if (reviewOpen) {
        MetadataReviewDialog(
            tracks = uploadedTracks,
            onDismiss = { reviewOpen = false },
        )
    }
}

@Composable
private fun UploadRow(
    entry: UploadEntry,
    canReplace: (Track) -> Boolean = { false },
    onReplace: (String) -> Unit = {},
    onAddAnyway: () -> Unit = {},
    onSkip: () -> Unit = {},
) {
    Column(Modifier.fillMaxWidth()) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(entry.file.name, style = MaterialTheme.typography.bodyLarge, maxLines = 1)
            val meta = buildString {
                append(formatBytes(entry.file.sizeBytes))
                entry.file.durationSec?.let { append(" • ${formatDuration(it)}") }
                when (entry.status) {
                    UploadStatus.UPLOADING -> append(" • uploading…")
                    UploadStatus.DONE -> append(" • done")
                    UploadStatus.REPLACED -> append(" • replaced")
                    UploadStatus.FAILED -> append(" • failed")
                    UploadStatus.DUPLICATE -> append(" • already in the library?")
                    UploadStatus.SKIPPED -> append(" • skipped")
                    UploadStatus.PENDING -> {}
                }
            }
            Text(
                meta,
                style = MaterialTheme.typography.bodySmall,
                color = when (entry.status) {
                    UploadStatus.DONE, UploadStatus.REPLACED -> Emerald
                    UploadStatus.FAILED -> MaterialTheme.colorScheme.error
                    UploadStatus.DUPLICATE -> MaterialTheme.colorScheme.tertiary
                    else -> MaterialTheme.colorScheme.onSurfaceVariant
                },
                maxLines = 1,
            )
            entry.error?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    maxLines = 2,
                )
            }
        }
        when (entry.status) {
            UploadStatus.PENDING -> {}
            UploadStatus.UPLOADING -> CircularProgressIndicator(
                modifier = Modifier.height(20.dp),
                strokeWidth = 2.dp,
            )
            UploadStatus.DONE, UploadStatus.REPLACED -> Icon(
                Icons.Filled.Check,
                contentDescription = null,
                tint = Emerald,
            )
            UploadStatus.FAILED, UploadStatus.DUPLICATE, UploadStatus.SKIPPED -> {}
        }
    }
    entry.duplicate?.let { dup ->
        Column(
            Modifier
                .fillMaxWidth()
                .padding(start = 8.dp, bottom = 4.dp),
        ) {
            Text(
                "This file • ${qualityLabel(dup.upload.mimeType, dup.upload.fileSize, dup.upload.duration)}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            dup.matches.forEach { m ->
                Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
                    CoverImage(m.track, 36.dp)
                    Spacer(Modifier.padding(start = 10.dp))
                    Column(Modifier.weight(1f)) {
                        Text(m.track.title, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOfNotNull(
                                m.track.artist.ifBlank { "Unknown artist" },
                                m.track.album.takeIf { it.isNotBlank() },
                                formatDuration(m.track.duration),
                                "by ${m.track.uploadedBy.username.ifBlank { "someone" }}",
                                m.reason,
                            ).joinToString(" • "),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            qualityLabel(m.track.mimeType, m.track.fileSize, m.track.duration),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                        )
                    }
                    if (canReplace(m.track)) {
                        TextButton(onClick = { onReplace(m.track.id) }) { Text("Replace") }
                    }
                }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onSkip) { Text("Skip") }
                OutlinedButton(onClick = onAddAnyway) { Text("Add anyway") }
            }
        }
    }
    }
}

private val formatNames = mapOf(
    "audio/mpeg" to "MP3",
    "audio/mp4" to "AAC",
    "audio/aac" to "AAC",
    "audio/flac" to "FLAC",
    "audio/wav" to "WAV",
    "audio/ogg" to "OGG",
    "audio/webm" to "WebM",
)

private fun qualityLabel(mimeType: String, fileSize: Long, duration: Double): String {
    val format = formatNames[mimeType] ?: mimeType.removePrefix("audio/").uppercase()
    val kbps = if (duration > 0 && fileSize > 0) (fileSize * 8 / duration / 1000).roundToInt() else 0
    return if (kbps > 0) "$format • $kbps kbps" else format
}
