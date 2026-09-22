package com.music.app.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Checkbox
import com.music.app.DeepLinks
import com.music.app.LocalAppModel
import com.music.app.api.defaultBaseUrl
import com.music.app.api.friendlyError
import com.music.app.state.InsecureServerException
import com.music.app.state.insecureServerWarning
import com.music.app.ui.theme.Emerald
import kotlinx.coroutines.launch

/**
 * Login / register (toggle) + server URL configuration. A fresh installation
 * points at [defaultBaseUrl]; another server can be typed here at any time
 * and is persisted in TokenStore. What goes in is the site's origin — the
 * app appends "/api/…" itself.
 */
@Composable
fun LoginScreen() {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()

    // An invite link (`/register?invite=CODE`) that opened the app lands here
    // with the code filled in and the form already on "create account".
    val linkedInvite = remember { DeepLinks.consumeInvite() }
    var registerMode by remember { mutableStateOf(linkedInvite != null) }
    var serverUrl by remember { mutableStateOf(model.api.baseUrl) }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var inviteCode by remember { mutableStateOf(linkedInvite ?: "") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    // Plain http off the LAN sends the password and session in the clear;
    // the user has to say so explicitly before we'll do it.
    val insecureWarning = insecureServerWarning(serverUrl)
    var acceptInsecure by remember(serverUrl) { mutableStateOf(false) }

    // Ask the server what sign-up needs before showing the form. Re-asked when
    // the server URL changes, since policy belongs to the server, not the app.
    LaunchedEffect(serverUrl) {
        if (serverUrl.isNotBlank()) model.loadAuthConfig(serverUrl)
    }
    val authConfig = model.authConfig

    fun submit() {
        if (busy) return
        scope.launch {
            busy = true
            error = null
            try {
                if (registerMode) {
                    if (authConfig.inviteRequired && inviteCode.isBlank()) {
                        error = "An invite code is required to sign up on this server."
                        busy = false
                        return@launch
                    }
                    if (password.length < authConfig.minPasswordLength) {
                        error = "Password must be at least ${authConfig.minPasswordLength} characters."
                        busy = false
                        return@launch
                    }
                    model.register(serverUrl, username, password, inviteCode, acceptInsecure)
                }
                else model.login(serverUrl, username, password, acceptInsecure)
                // on success the root switches to MainScreen automatically
            } catch (e: InsecureServerException) {
                error = e.message
            } catch (e: Exception) {
                error = friendlyError(e)
            } finally {
                busy = false
            }
        }
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(48.dp))
        Text(
            "Music",
            style = MaterialTheme.typography.displayMedium,
            fontWeight = FontWeight.Bold,
            color = Emerald,
        )
        Spacer(Modifier.height(40.dp))

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server URL") },
            placeholder = { Text(defaultBaseUrl) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = username,
            onValueChange = { username = it },
            label = { Text("Username") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next),
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Password") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Password,
                imeAction = ImeAction.Done,
            ),
            modifier = Modifier.fillMaxWidth(),
        )
        // Always offered when signing up: a single-use invite link's code gets
        // its holder in even while sign-ups are closed, so the field can't be
        // hidden behind the server's general policy.
        if (registerMode) {
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = inviteCode,
                onValueChange = { inviteCode = it },
                label = { Text(if (authConfig.inviteRequired || !authConfig.registrationOpen) "Invite code" else "Invite code (optional)") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                modifier = Modifier.fillMaxWidth(),
            )
        }

        if (insecureWarning != null) {
            Spacer(Modifier.height(12.dp))
            Text(
                insecureWarning,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = acceptInsecure, onCheckedChange = { acceptInsecure = it })
                Text("Connect anyway", style = MaterialTheme.typography.bodyMedium)
            }
        }

        error?.let { message ->
            Spacer(Modifier.height(12.dp))
            Text(
                message,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodyMedium,
            )
        }

        Spacer(Modifier.height(28.dp))
        Button(
            onClick = { submit() },
            enabled = !busy && username.isNotBlank() && password.isNotBlank(),
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp),
        ) {
            if (busy) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
            } else {
                Text(if (registerMode) "Create account" else "Sign in")
            }
        }
        TextButton(
            onClick = {
                registerMode = !registerMode
                error = null
            },
        ) {
            Text(if (registerMode) "Sign in" else "Create an account")
        }
    }
}
