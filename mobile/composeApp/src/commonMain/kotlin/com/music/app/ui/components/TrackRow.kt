package com.music.app.ui.components

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.TextButton
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import com.music.app.api.friendlyError
import com.music.app.plainTextClipEntry
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.offline.DownloadState
import com.music.app.ui.formatDuration
import com.music.app.ui.formatTimeUntil
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc500
import kotlinx.coroutines.launch

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun TrackRow(
    track: Track,
    context: List<Track>,
    modifier: Modifier = Modifier,
    showActions: Boolean = true,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    val model = LocalAppModel.current
    val mine = model.user?.let { it.id == track.uploadedBy.id || it.isAdmin } == true
    var menuOpen by remember { mutableStateOf(false) }
    var reviewOpen by remember { mutableStateOf(false) }
    var sharing by remember(track.id) { mutableStateOf(false) }
    var shareMessage by remember(track.id) { mutableStateOf<String?>(null) }
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    val lookingUp = track.metadataStatus == "queued" || track.metadataStatus == "running"

    Box(modifier) {
        Row(
            Modifier
                .fillMaxWidth()
                .combinedClickable(
                    onClick = { model.playTrack(track, context) },
                    onLongClick = { menuOpen = true },
                )
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CoverImage(track, 48.dp)
            Column(
                Modifier
                    .weight(1f)
                    .padding(horizontal = 12.dp),
            ) {
                Text(
                    track.title,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onBackground,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (lookingUp) {
                        CircularProgressIndicator(Modifier.size(10.dp), strokeWidth = 1.5.dp, color = Emerald)
                        Spacer(Modifier.width(6.dp))
                    }
                    Text(
                        trackSubtitle(track),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            if (trailing != null) trailing()
            if (showActions && track.uploadedBy.id.isNotEmpty()) {
                UserAvatar(
                    userId = track.uploadedBy.id,
                    username = track.uploadedBy.username,
                    hasAvatar = track.uploadedBy.hasAvatar,
                    size = 18.dp,
                    label = "Uploaded by ${track.uploadedBy.username.ifBlank { "someone" }}",
                    modifier = Modifier.alpha(0.75f),
                )
                Spacer(Modifier.width(6.dp))
            }
            if (showActions) {
                DownloadButton(track)
                IconButton(onClick = { model.toggleFavorite(track) }) {
                    Icon(
                        imageVector = if (track.isFavorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                        contentDescription = if (track.isFavorite) "Remove from favorites" else "Add to favorites",
                        tint = if (track.isFavorite) Emerald else Zinc500,
                    )
                }
            }
        }
        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
            if (model.listen.session?.canControl == true) {
                DropdownMenuItem(
                    text = { Text("Play next") },
                    onClick = {
                        menuOpen = false
                        model.listen.add(listOf(track), "next")
                    },
                )
                DropdownMenuItem(
                    text = { Text("Add to queue") },
                    onClick = {
                        menuOpen = false
                        model.listen.add(listOf(track), "end")
                    },
                )
            }
            DropdownMenuItem(
                text = { Text(if (sharing) "Copying link…" else "Copy share link") },
                enabled = !sharing,
                onClick = {
                    sharing = true
                    scope.launch {
                        try {
                            val (link, expiresAt) = model.shareLinkFor(track)
                            clipboard.setClipEntry(plainTextClipEntry(link))
                            shareMessage = "Link copied" + (expiresAt?.let { " · expires ${formatTimeUntil(it)}" } ?: "")
                        } catch (e: Exception) {
                            shareMessage = friendlyError(e)
                        } finally {
                            sharing = false
                            menuOpen = false
                        }
                    }
                },
            )
            DropdownMenuItem(
                text = { Text("Stop sharing") },
                onClick = {
                    menuOpen = false
                    scope.launch {
                        shareMessage = try {
                            model.stopSharing(track)
                            "Link turned off"
                        } catch (e: Exception) {
                            friendlyError(e)
                        }
                    }
                },
            )
            if (mine) {
                DropdownMenuItem(
                    text = { Text("Check track details") },
                    onClick = {
                        menuOpen = false
                        reviewOpen = true
                    },
                )
            }
        }
    }

    if (reviewOpen) {
        MetadataReviewDialog(tracks = listOf(track), onDismiss = { reviewOpen = false })
    }

    shareMessage?.let { message ->
        AlertDialog(
            onDismissRequest = { shareMessage = null },
            text = { Text(message) },
            confirmButton = { TextButton(onClick = { shareMessage = null }) { Text("OK") } },
        )
    }

}

fun trackSubtitle(track: Track): String {
    val artist = track.artist.ifBlank { "Unknown artist" }
    return "$artist · ${formatDuration(track.duration)}"
}

@Composable
fun DownloadButton(track: Track, modifier: Modifier = Modifier) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val state = model.downloads.stateOf(track.id)
    var chooseVersion by remember(track.id) { mutableStateOf(false) }

    fun start(useCompatible: Boolean?) {
        model.downloads.clearFailure(track.id)
        scope.launch { model.downloads.download(track, useCompatible) }
    }

    IconButton(
        modifier = modifier,
        onClick = {
            when (state) {
                DownloadState.DONE -> model.downloads.remove(track.id)
                DownloadState.DOWNLOADING -> Unit
                else -> if (track.hasCompatible) chooseVersion = true else start(null)
            }
        },
    ) {
        when (state) {
            DownloadState.DOWNLOADING -> CircularProgressIndicator(
                modifier = Modifier.size(18.dp),
                strokeWidth = 2.dp,
                color = Emerald,
            )
            DownloadState.DONE -> Icon(
                Icons.Filled.DownloadDone,
                contentDescription = "Downloaded — tap to remove",
                tint = Emerald,
            )
            DownloadState.FAILED -> Icon(
                Icons.Filled.ErrorOutline,
                contentDescription = "Download failed — tap to retry",
                tint = MaterialTheme.colorScheme.error,
            )
            DownloadState.NONE -> Icon(
                Icons.Filled.Download,
                contentDescription = "Download for offline",
                tint = Zinc500,
            )
        }
    }

    if (chooseVersion) {
        DownloadVersionDialog(
            track = track,
            onDismiss = { chooseVersion = false },
            onChoose = { useCompatible ->
                chooseVersion = false
                start(useCompatible)
            },
        )
    }
}


@Composable
private fun DownloadVersionDialog(
    track: Track,
    onDismiss: () -> Unit,
    onChoose: (useCompatible: Boolean) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Download which version?") },
        text = {
            Column {
                Text(
                    track.title,
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        },
        confirmButton = {
            TextButton(onClick = { onChoose(false) }) {
                Text("Original (${shortFormat(track.mimeType)})")
            }
        },
        dismissButton = {
            TextButton(onClick = { onChoose(true) }) {
                Text("Plays on any platform (${shortFormat(track.compatibleMimeType)})")
            }
        },
    )
}

private fun shortFormat(mime: String): String = when (mime.substringBefore(';').trim().lowercase()) {
    "audio/mpeg", "audio/mp3" -> "MP3"
    "audio/mp4", "audio/m4a", "audio/x-m4a" -> "AAC"
    "audio/flac", "audio/x-flac" -> "FLAC"
    "audio/ogg", "application/ogg" -> "OGG"
    "audio/opus" -> "Opus"
    "audio/webm" -> "WebM"
    "audio/x-ms-wma" -> "WMA"
    "audio/wav", "audio/x-wav", "audio/wave" -> "WAV"
    "audio/aiff", "audio/x-aiff" -> "AIFF"
    else -> mime.substringAfter('/').uppercase()
}
