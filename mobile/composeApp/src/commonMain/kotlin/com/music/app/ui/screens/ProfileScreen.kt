package com.music.app.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ExitToApp
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Key
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Storage
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.friendlyError
import com.music.app.ui.components.AvatarEditor
import com.music.app.ui.components.DownloadQualitySetting
import com.music.app.ui.components.SyncSettings
import com.music.app.ui.theme.Emerald
import kotlinx.coroutines.launch

/**
 * The signed-in user's page: picture, account, download preferences and the
 * way out. Opened from the avatar in the Home header. Anything that is a
 * setting rather than a piece of the library belongs here.
 */
@Composable
fun ProfileScreen() {
    val model = LocalAppModel.current
    val user = model.user ?: return
    val scope = rememberCoroutineScope()

    var passwordOpen by remember { mutableStateOf(false) }
    var logoutOpen by remember { mutableStateOf(false) }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
    ) {
        // ---- header ----
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 4.dp, end = 8.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Text(
                "Profile",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }

        // ---- identity ----
        Column(
            Modifier
                .fillMaxWidth()
                .padding(vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            AvatarEditor(size = 96.dp)
            Spacer(Modifier.height(12.dp))
            Text(
                user.username,
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (user.isAdmin) {
                    Icon(Icons.Filled.Shield, contentDescription = null, tint = Emerald, modifier = Modifier.size(14.dp))
                    Spacer(Modifier.width(4.dp))
                }
                Text(
                    buildString {
                        append(if (user.isAdmin) "Admin" else "Member")
                        if (user.createdAt.isNotBlank()) append(" · joined ${user.createdAt.take(10)}")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                model.api.baseUrl,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 2.dp),
            )
        }

        // ---- listening ----
        SectionCard("Listening") {
            SettingRow(
                icon = Icons.Filled.History,
                title = "History",
                onClick = { model.push(Screen.History) },
            )
        }

        // ---- account ----
        SectionCard("Account") {
            SettingRow(
                icon = Icons.Filled.Key,
                title = "Change password",
                onClick = { passwordOpen = true },
            )
            HorizontalDivider()
            SettingRow(
                icon = Icons.AutoMirrored.Filled.ExitToApp,
                title = "Sign out",
                onClick = { logoutOpen = true },
            )
        }

        // ---- download preferences ----
        SectionCard("Download preferences", icon = Icons.Filled.Storage) {
            DownloadQualitySetting()
            SyncSettings()
        }

        Spacer(Modifier.height(24.dp))
    }

    if (passwordOpen) {
        ChangePasswordDialog(onDismiss = { passwordOpen = false })
    }

    if (logoutOpen) {
        var everywhereError by remember { mutableStateOf<String?>(null) }
        var busy by remember { mutableStateOf(false) }
        AlertDialog(
            onDismissRequest = { if (!busy) logoutOpen = false },
            title = { Text("Sign out?") },
            text = {
                Column {
                    Text("Downloads on this phone are removed.")
                    everywhereError?.let {
                        Spacer(Modifier.height(8.dp))
                        Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                    }
                }
            },
            confirmButton = {
                Column(horizontalAlignment = Alignment.End) {
                    TextButton(enabled = !busy, onClick = {
                        logoutOpen = false
                        model.logout()
                    }) { Text("Sign out here") }
                    TextButton(enabled = !busy, onClick = {
                        scope.launch {
                            busy = true
                            try {
                                model.logoutEverywhere()
                                logoutOpen = false
                            } catch (e: Exception) {
                                everywhereError = friendlyError(e)
                            } finally {
                                busy = false
                            }
                        }
                    }) { Text("Sign out everywhere", color = MaterialTheme.colorScheme.error) }
                }
            },
            dismissButton = {
                TextButton(enabled = !busy, onClick = { logoutOpen = false }) { Text("Cancel") }
            },
        )
    }
}

@Composable
private fun SectionCard(
    title: String,
    icon: ImageVector? = null,
    content: @Composable () -> Unit,
) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 4.dp, bottom = 6.dp)) {
            if (icon != null) {
                Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(14.dp))
                Spacer(Modifier.width(6.dp))
            }
            Text(
                title.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Surface(
            shape = RoundedCornerShape(16.dp),
            color = MaterialTheme.colorScheme.surface,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Column(Modifier.padding(vertical = 4.dp)) { content() }
        }
    }
}

@Composable
private fun SettingRow(
    icon: ImageVector,
    title: String,
    onClick: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(icon, contentDescription = null, tint = Emerald, modifier = Modifier.size(22.dp))
        Text(title, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
        Icon(
            Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** Current + new password; the server revokes every other session on success. */
@Composable
private fun ChangePasswordDialog(onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var current by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var done by remember { mutableStateOf(false) }

    val minLength = model.authConfig.minPasswordLength

    fun submit() {
        if (busy) return
        if (next.length < minLength) {
            error = "New password must be at least $minLength characters."
            return
        }
        if (next != confirm) {
            error = "New passwords don't match."
            return
        }
        scope.launch {
            busy = true
            error = null
            try {
                model.api.changePassword(current, next)
                done = true
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = false
            }
        }
    }

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(if (done) "Password changed" else "Change password") },
        text = {
            if (done) {
                Text("Other devices were signed out.")
            } else {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    OutlinedTextField(
                        value = current,
                        onValueChange = { current = it },
                        label = { Text("Current password") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Next),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = next,
                        onValueChange = { next = it },
                        label = { Text("New password") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Next),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = confirm,
                        onValueChange = { confirm = it },
                        label = { Text("Confirm new password") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    error?.let {
                        Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        },
        confirmButton = {
            if (done) {
                TextButton(onClick = onDismiss) { Text("Done") }
            } else {
                TextButton(
                    enabled = !busy && current.isNotBlank() && next.isNotBlank() && confirm.isNotBlank(),
                    onClick = { submit() },
                ) {
                    if (busy) {
                        Box(Modifier.size(18.dp)) { CircularProgressIndicator(strokeWidth = 2.dp) }
                    } else {
                        Text("Update")
                    }
                }
            }
        },
        dismissButton = {
            if (!done) TextButton(enabled = !busy, onClick = onDismiss) { Text("Cancel") }
        },
    )
}
