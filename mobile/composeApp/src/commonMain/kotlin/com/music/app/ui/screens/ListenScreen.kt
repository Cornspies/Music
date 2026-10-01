package com.music.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.PersonRemove
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.DeepLinks
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.ListenEntry
import com.music.app.api.ListenInvite
import com.music.app.api.ListenMember
import com.music.app.api.ListenState
import com.music.app.api.friendlyError
import com.music.app.plainTextClipEntry
import com.music.app.shareText
import com.music.app.state.DEFAULT_LISTEN_GUESTS
import com.music.app.state.MAX_LISTEN_GUESTS
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.UserAvatar
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.OnEmerald
import com.music.app.ui.theme.Zinc400
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc600
import kotlinx.coroutines.launch

@Composable
fun ListenScreen() {
    val model = LocalAppModel.current
    val session = model.listen.session
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
            Text(
                "Listen together",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }
        if (session == null) StartListening() else SessionDetails(session)
    }
}

@Composable
private fun StartListening() {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var capacity by remember { mutableIntStateOf(DEFAULT_LISTEN_GUESTS) }
    var everyone by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val current = model.currentTrack

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            "Share a link, and everyone who joins hears the same music as you, at the same moment. " +
                "Pausing only pauses it for you. They need an account on this server.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        ListenCard {
            SettingLine("People who can join", "Not counting you") {
                Stepper(capacity, onChange = { capacity = it })
            }
            HorizontalDivider()
            SettingLine("Everyone can add songs and skip", "Otherwise only you choose what plays.") {
                EmeraldSwitch(everyone, onChange = { everyone = it })
            }
        }
        Text(
            if (current != null) {
                "Starts with “${current.title}” and what's queued after it."
            } else {
                "Starts empty. Add songs once it's going: tap a song to play it for everyone."
            },
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        Button(
            onClick = {
                busy = true
                error = null
                scope.launch {
                    try {
                        model.startListening(capacity, everyone)
                    } catch (e: Exception) {
                        error = friendlyError(e)
                    } finally {
                        busy = false
                    }
                }
            },
            enabled = !busy && !model.offlineMode,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(containerColor = Emerald, contentColor = OnEmerald),
        ) {
            if (busy) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = OnEmerald)
            } else {
                Icon(Icons.Filled.Groups, contentDescription = null, modifier = Modifier.size(18.dp))
            }
            Spacer(Modifier.width(8.dp))
            Text("Start session")
        }
        if (model.offlineMode) {
            Text(
                "You're offline.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun SessionDetails(session: ListenState) {
    val model = LocalAppModel.current
    val listen = model.listen
    val me = model.user?.id
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboard.current
    var confirmEnd by remember { mutableStateOf(false) }
    var confirmClear by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    val guests = session.members.size - 1
    val upcoming = listen.upcoming()

    fun leave() {
        busy = true
        model.scope.launch {
            try {
                listen.leave()
            } catch (e: Exception) {
                model.notify(friendlyError(e))
            } finally {
                busy = false
            }
        }
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        item {
            Text(
                if (session.isHost) {
                    "You're hosting · $guests of ${session.capacity} joined"
                } else {
                    "Hosted by ${session.host.username}"
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp),
            )
        }
        val code = session.code
        if (session.isHost && code != null) {
            item {
                val link = DeepLinks.listenUrl(model.api.baseUrl, code)
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    ListenCard {
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            Text(
                                link,
                                style = MaterialTheme.typography.bodySmall,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                Button(
                                    onClick = { shareText(link) },
                                    colors = ButtonDefaults.buttonColors(containerColor = Emerald, contentColor = OnEmerald),
                                    modifier = Modifier.weight(1f),
                                ) {
                                    Icon(Icons.Filled.Share, contentDescription = null, modifier = Modifier.size(18.dp))
                                    Spacer(Modifier.width(8.dp))
                                    Text("Share link")
                                }
                                OutlinedButton(
                                    onClick = {
                                        scope.launch {
                                            clipboard.setClipEntry(plainTextClipEntry(link))
                                            model.notify("Invite link copied")
                                        }
                                    },
                                ) {
                                    Icon(Icons.Filled.ContentCopy, contentDescription = null, modifier = Modifier.size(18.dp))
                                    Spacer(Modifier.width(8.dp))
                                    Text("Copy")
                                }
                            }
                            Text(
                                "Anyone with an account here can join with this link.",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }

        item { SectionTitle("People") }
        items(session.members, key = { it.id }) { member ->
            MemberRow(
                member,
                isMe = member.id == me,
                onRemove = if (session.isHost && !member.isHost) ({ listen.removeMember(member.id) }) else null,
            )
        }

        item {
            if (session.isHost) {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    ListenCard {
                        SettingLine("People who can join", null) {
                            Stepper(session.capacity, onChange = { listen.updateSettings(capacity = it) })
                        }
                        HorizontalDivider()
                        SettingLine("Everyone can add songs and skip", null) {
                            EmeraldSwitch(
                                session.everyoneCanControl,
                                onChange = { listen.updateSettings(everyoneCanControl = it) },
                            )
                        }
                    }
                    Text(
                        "Pausing only pauses it for you. Skipping moves everyone on.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 4.dp, top = 8.dp),
                    )
                }
            } else {
                Text(
                    if (session.everyoneCanControl) {
                        "${session.host.username} lets everyone add songs and skip. Pausing only pauses it for you."
                    } else {
                        "${session.host.username} chooses what plays. Pausing only pauses it for you."
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
                )
            }
        }

        item {
            Row(
                Modifier.fillMaxWidth().padding(end = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.weight(1f)) {
                    SectionTitle(if (upcoming.isEmpty()) "Up next" else "Up next · ${upcoming.size}")
                }
                if (session.isHost && upcoming.isNotEmpty()) {
                    TextButton(onClick = { confirmClear = true }) {
                        Text("Clear queue", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
        if (upcoming.isEmpty()) {
            item {
                Text(
                    if (session.canControl) {
                        "Nothing queued. Tap a song to play it for everyone, hold it to queue it, " +
                            "or add a whole playlist from its page."
                    } else {
                        "Nothing queued."
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp),
                )
            }
        }
        items(upcoming, key = { it.id }) { entry ->
            QueueEntryRow(
                entry,
                onPlay = if (session.canControl) ({ listen.jump(entry) }) else null,
                onRemove = if (listen.canRemove(entry)) ({ listen.remove(entry) }) else null,
            )
        }

        item {
            TextButton(
                onClick = { if (session.isHost) confirmEnd = true else leave() },
                enabled = !busy,
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 16.dp),
            ) {
                Icon(
                    Icons.AutoMirrored.Filled.Logout,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.error,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(8.dp))
                Text(if (session.isHost) "End session" else "Leave session", color = MaterialTheme.colorScheme.error)
            }
        }
    }

    if (confirmClear) {
        AlertDialog(
            onDismissRequest = { confirmClear = false },
            title = { Text("Clear the queue?") },
            text = {
                Text(
                    (if (upcoming.size == 1) "The song" else "The ${upcoming.size} songs") +
                        " after the one playing now " + (if (upcoming.size == 1) "is" else "are") +
                        " removed for everyone in the session. What's playing carries on.",
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmClear = false
                    listen.clear()
                }) { Text("Clear queue", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { confirmClear = false }) { Text("Cancel") } },
        )
    }

    if (confirmEnd) {
        AlertDialog(
            onDismissRequest = { confirmEnd = false },
            title = { Text("End the session?") },
            text = { Text("The music stops following along for everyone who joined.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmEnd = false
                    leave()
                }) { Text("End session", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { confirmEnd = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun MemberRow(member: ListenMember, isMe: Boolean, onRemove: (() -> Unit)?) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box {
            UserAvatar(member.id, member.username, member.hasAvatar, size = 36.dp)
            Box(
                Modifier
                    .align(Alignment.BottomEnd)
                    .size(11.dp)
                    .clip(CircleShape)
                    .background(MaterialTheme.colorScheme.background)
                    .padding(2.dp)
                    .clip(CircleShape)
                    .background(if (member.online) Emerald else Zinc600),
            )
        }
        Column(
            Modifier
                .weight(1f)
                .padding(horizontal = 12.dp),
        ) {
            Text(
                member.username + if (isMe) " (you)" else "",
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                (if (member.isHost) "Host · " else "") + if (member.online) "Listening" else "Away",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (member.isHost) {
            Icon(Icons.Filled.Star, contentDescription = "Host", tint = Emerald, modifier = Modifier.size(18.dp))
        }
        if (onRemove != null) {
            IconButton(onClick = onRemove) {
                Icon(Icons.Filled.PersonRemove, contentDescription = "Remove ${member.username}", tint = Zinc500)
            }
        }
    }
}

@Composable
private fun QueueEntryRow(entry: ListenEntry, onPlay: (() -> Unit)?, onRemove: (() -> Unit)?) {
    val track = entry.track
    Row(
        Modifier
            .fillMaxWidth()
            .then(if (onPlay != null) Modifier.clickable(onClick = onPlay) else Modifier)
            .padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CoverImage(track, 44.dp)
        Column(
            Modifier
                .weight(1f)
                .padding(horizontal = 12.dp),
        ) {
            Text(track.title, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                track.artist.ifBlank { "Unknown artist" },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        UserAvatar(
            userId = entry.addedBy.id,
            username = entry.addedBy.username,
            hasAvatar = entry.addedBy.hasAvatar,
            size = 20.dp,
            label = "Added by ${entry.addedBy.username}",
        )
        if (onRemove != null) {
            IconButton(onClick = onRemove) {
                Icon(Icons.Filled.Close, contentDescription = "Remove ${track.title}", tint = Zinc500)
            }
        } else {
            Spacer(Modifier.width(16.dp))
        }
    }
}

@Composable
private fun ListenCard(content: @Composable () -> Unit) {
    Surface(
        shape = RoundedCornerShape(16.dp),
        color = MaterialTheme.colorScheme.surface,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column { content() }
    }
}

@Composable
private fun SectionTitle(title: String) {
    Text(
        title.uppercase(),
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 16.dp, bottom = 6.dp),
    )
}

@Composable
private fun SettingLine(title: String, subtitle: String?, control: @Composable () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            if (subtitle != null) {
                Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        control()
    }
}

@Composable
private fun Stepper(value: Int, onChange: (Int) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { onChange(value - 1) }, enabled = value > 1) {
            Icon(Icons.Filled.Remove, contentDescription = "Fewer people")
        }
        Text(
            value.toString(),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.width(28.dp),
            textAlign = TextAlign.Center,
        )
        IconButton(onClick = { onChange(value + 1) }, enabled = value < MAX_LISTEN_GUESTS) {
            Icon(Icons.Filled.Add, contentDescription = "More people")
        }
    }
}

@Composable
private fun EmeraldSwitch(checked: Boolean, onChange: (Boolean) -> Unit) {
    Switch(
        checked = checked,
        onCheckedChange = onChange,
        colors = SwitchDefaults.colors(checkedTrackColor = Emerald, checkedThumbColor = OnEmerald),
    )
}

@Composable
fun ListenInviteDialog(code: String, onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    var invite by remember(code) { mutableStateOf<ListenInvite?>(null) }
    var error by remember(code) { mutableStateOf<String?>(null) }
    var busy by remember(code) { mutableStateOf(false) }
    LaunchedEffect(code) {
        try {
            invite = model.listen.invite(code)
        } catch (e: Exception) {
            error = friendlyError(e)
        }
    }
    val current = model.listen.session
    val found = invite
    val alreadyHere = found != null && current?.id == found.id
    val full = found != null && found.full && !found.joined

    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Filled.Groups, contentDescription = null, tint = Emerald) },
        title = { Text("Listen together") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                when {
                    error != null -> Text(error!!.replaceFirstChar { it.uppercase() })
                    found == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                    }
                    else -> {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            UserAvatar(found.host.id, found.host.username, found.host.hasAvatar, size = 36.dp)
                            Column(Modifier.padding(start = 12.dp)) {
                                Text("${found.host.username} invited you to listen along.")
                                Text(
                                    "${found.guests} of ${found.capacity} joined",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Zinc400,
                                )
                            }
                        }
                        found.nowPlaying?.let { np ->
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Icon(Icons.Filled.MusicNote, contentDescription = null, tint = Emerald, modifier = Modifier.size(16.dp))
                                Spacer(Modifier.width(6.dp))
                                Text(
                                    np.title + (np.artist.takeIf { it.isNotBlank() }?.let { " · $it" } ?: ""),
                                    style = MaterialTheme.typography.bodySmall,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        when {
                            full -> Text("This session is full.", color = MaterialTheme.colorScheme.error)
                            current != null && !alreadyHere -> Text(
                                if (current.isHost) "Joining ends the session you're hosting." else "You'll leave the session you're in now.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.error,
                            )
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = found != null && !busy && !full,
                onClick = {
                    val target = found ?: return@TextButton
                    busy = true
                    model.scope.launch {
                        try {
                            when {
                                alreadyHere -> Unit
                                target.joined -> model.listen.restore(followNow = true)
                                else -> model.listen.join(code)
                            }
                            onDismiss()
                            model.push(if (model.currentTrack != null) Screen.NowPlaying else Screen.Listen)
                        } catch (e: Exception) {
                            error = friendlyError(e)
                        } finally {
                            busy = false
                        }
                    }
                },
            ) { Text(if (alreadyHere || found?.joined == true) "Open" else "Join") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Not now") } },
    )
}
