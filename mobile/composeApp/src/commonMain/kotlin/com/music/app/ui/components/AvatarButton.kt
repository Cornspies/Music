package com.music.app.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.PickedImage
import com.music.app.canPickImageFromDevice
import com.music.app.readBytes
import com.music.app.rememberImagePickerLauncher
import com.music.app.api.friendlyError
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Largest avatar the server accepts. */
private const val MAX_AVATAR_BYTES = 8 * 1024 * 1024

/**
 * The signed-in user's avatar in the Home header, doubling as the control for
 * setting or clearing it. On iOS (no image picker wired up yet) it's display
 * only, with a hint pointing at the web app.
 */
@Composable
fun AvatarButton() {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val user = model.user ?: return

    var sheetOpen by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    /** Image chosen but not yet framed and uploaded. */
    var pending by remember { mutableStateOf<Pair<PickedImage, ByteArray>?>(null) }

    // Picking an image opens the crop dialog; the upload happens once the
    // user has framed it.
    fun prepare(picked: PickedImage) {
        scope.launch {
            busy = true
            error = null
            try {
                val bytes = withContext(Dispatchers.Default) { picked.readBytes() }
                if (bytes.size > MAX_AVATAR_BYTES) {
                    error = "That image is larger than 8 MB."
                } else {
                    pending = picked to bytes
                }
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = false
            }
        }
    }

    fun upload(crop: AvatarCrop) {
        val (picked, bytes) = pending ?: return
        scope.launch {
            busy = true
            error = null
            try {
                model.updateAvatar(picked.name, picked.mimeType, bytes, crop)
                pending = null
                sheetOpen = false
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = false
            }
        }
    }

    val pickImage = rememberImagePickerLauncher { picked -> prepare(picked) }

    Box(
        Modifier
            .size(40.dp)
            .clickable { sheetOpen = true },
        contentAlignment = Alignment.Center,
    ) {
        UserAvatar(
            userId = user.id,
            username = user.username,
            hasAvatar = user.hasAvatar,
            size = 28.dp,
            label = "Your picture",
        )
        if (busy) {
            CircularProgressIndicator(modifier = Modifier.size(28.dp), strokeWidth = 2.dp)
        }
    }

    pending?.let { (_, bytes) ->
        AvatarCropDialog(
            bytes = bytes,
            busy = busy,
            onCancel = { pending = null },
            onConfirm = { crop -> upload(crop) },
        )
    }

    if (sheetOpen) {
        AlertDialog(
            onDismissRequest = { if (!busy) sheetOpen = false },
            title = { Text("Your picture") },
            text = {
                Column(Modifier.fillMaxWidth()) {
                    Text(
                        if (canPickImageFromDevice) {
                            "Shown next to every track you upload."
                        } else {
                            "Shown next to every track you upload. Setting a picture " +
                                "isn't available on iOS yet — use the Music web app."
                        },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    error?.let {
                        Text(
                            it,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                            modifier = Modifier.padding(top = 8.dp),
                        )
                    }
                }
            },
            confirmButton = {
                if (canPickImageFromDevice) {
                    TextButton(onClick = { pickImage() }, enabled = !busy) {
                        Text(if (user.hasAvatar) "Change picture" else "Choose picture")
                    }
                } else {
                    TextButton(onClick = { sheetOpen = false }) { Text("Close") }
                }
            },
            dismissButton = {
                if (user.hasAvatar) {
                    TextButton(
                        enabled = !busy,
                        onClick = {
                            scope.launch {
                                busy = true
                                error = null
                                try {
                                    model.clearAvatar()
                                    sheetOpen = false
                                } catch (e: Exception) {
                                    error = friendlyError(e)
                                } finally {
                                    busy = false
                                }
                            }
                        },
                    ) { Text("Remove") }
                }
            },
        )
    }
}
