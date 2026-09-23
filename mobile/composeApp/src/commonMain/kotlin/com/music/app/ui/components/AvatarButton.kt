package com.music.app.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.PickedImage
import com.music.app.canPickImageFromDevice
import com.music.app.readBytes
import com.music.app.rememberImagePickerLauncher
import com.music.app.api.friendlyError
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val MAX_AVATAR_BYTES = 8 * 1024 * 1024

@Composable
fun AvatarButton() {
    val model = LocalAppModel.current
    val user = model.user ?: return
    Box(
        Modifier
            .size(40.dp)
            .clickable { model.push(Screen.Profile) },
        contentAlignment = Alignment.Center,
    ) {
        UserAvatar(
            userId = user.id,
            username = user.username,
            hasAvatar = user.hasAvatar,
            size = 28.dp,
            label = "Your profile",
        )
    }
}

@Composable
fun AvatarEditor(size: Dp = 96.dp) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    val user = model.user ?: return

    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var pending by remember { mutableStateOf<Pair<PickedImage, ByteArray>?>(null) }

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
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = false
            }
        }
    }

    val pickImage = rememberImagePickerLauncher { picked -> prepare(picked) }

    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Box(
            Modifier
                .size(size)
                .clickable(enabled = canPickImageFromDevice && !busy) { pickImage() },
            contentAlignment = Alignment.Center,
        ) {
            UserAvatar(
                userId = user.id,
                username = user.username,
                hasAvatar = user.hasAvatar,
                size = size,
                label = "Your picture",
            )
            if (busy) {
                CircularProgressIndicator(modifier = Modifier.size(size / 3), strokeWidth = 2.dp)
            }
        }
        Row(horizontalArrangement = Arrangement.Center) {
            if (canPickImageFromDevice) {
                TextButton(onClick = { pickImage() }, enabled = !busy) {
                    Text(if (user.hasAvatar) "Change picture" else "Choose picture")
                }
            }
            if (user.hasAvatar) {
                TextButton(
                    enabled = !busy,
                    onClick = {
                        scope.launch {
                            busy = true
                            error = null
                            try {
                                model.clearAvatar()
                            } catch (e: Exception) {
                                error = friendlyError(e)
                            } finally {
                                busy = false
                            }
                        }
                    },
                ) { Text("Remove", color = MaterialTheme.colorScheme.error) }
            }
        }
        if (!canPickImageFromDevice) {
            Text(
                "Use the website to set it",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        error?.let {
            Text(
                it,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(top = 4.dp),
            )
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
}
