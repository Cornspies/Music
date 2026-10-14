package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import com.music.app.PickedImage
import com.music.app.api.friendlyError
import com.music.app.canPickImageFromDevice
import com.music.app.readBytes
import com.music.app.rememberImagePickerLauncher
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.EmeraldDark
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val MAX_COVER_BYTES = 8 * 1024 * 1024

@Composable
fun PlaylistArt(
    playlistId: String,
    coverVersion: String,
    size: Dp,
    modifier: Modifier = Modifier,
    corner: Dp = 10.dp,
) {
    val model = LocalAppModel.current
    Box(
        modifier
            .size(size)
            .clip(RoundedCornerShape(corner))
            .background(EmeraldDark.copy(alpha = 0.25f)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = null, tint = Emerald)
        if (coverVersion.isNotEmpty()) {
            AsyncImage(
                model = model.api.playlistCoverUrl(playlistId, coverVersion),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
fun PlaylistCoverEditor(
    playlistId: String,
    coverVersion: String,
    size: Dp,
    onError: (String?) -> Unit,
) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var menuOpen by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var pending by remember { mutableStateOf<Pair<PickedImage, ByteArray>?>(null) }

    fun run(block: suspend () -> Unit) {
        scope.launch {
            busy = true
            onError(null)
            try {
                block()
            } catch (e: Exception) {
                onError(friendlyError(e))
            } finally {
                busy = false
            }
        }
    }

    val pickImage = rememberImagePickerLauncher { picked ->
        run {
            val bytes = withContext(Dispatchers.Default) { picked.readBytes() }
            if (bytes.size > MAX_COVER_BYTES) {
                onError("That image is larger than 8 MB.")
            } else {
                pending = picked to bytes
            }
        }
    }

    val canEdit = canPickImageFromDevice || coverVersion.isNotEmpty()
    Box(contentAlignment = Alignment.Center) {
        PlaylistArt(
            playlistId,
            coverVersion,
            size,
            Modifier.clickable(enabled = canEdit && !busy) { menuOpen = true },
        )
        if (busy) CircularProgressIndicator(Modifier.size(size / 2), strokeWidth = 2.dp)
        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
            if (canPickImageFromDevice) {
                DropdownMenuItem(
                    text = { Text(if (coverVersion.isEmpty()) "Choose cover" else "Change cover") },
                    onClick = {
                        menuOpen = false
                        pickImage()
                    },
                )
            }
            if (coverVersion.isNotEmpty()) {
                DropdownMenuItem(
                    text = { Text("Remove cover", color = MaterialTheme.colorScheme.error) },
                    onClick = {
                        menuOpen = false
                        run { model.clearPlaylistCover(playlistId) }
                    },
                )
            }
        }
    }

    pending?.let { (picked, bytes) ->
        AvatarCropDialog(
            bytes = bytes,
            busy = busy,
            square = true,
            onCancel = { pending = null },
            onConfirm = { crop ->
                run {
                    model.setPlaylistCover(playlistId, picked.name, picked.mimeType, bytes, crop)
                    pending = null
                }
            },
        )
    }
}
