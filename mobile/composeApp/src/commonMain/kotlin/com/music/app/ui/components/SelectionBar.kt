package com.music.app.ui.components

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DoneAll
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.Track
import com.music.app.ui.theme.Emerald

@Composable
fun SelectionBar() {
    val model = LocalAppModel.current
    val count = model.selectedIds.size
    val total = model.selectionContext.distinctBy { it.id }.size
    var adding by remember { mutableStateOf<List<Track>?>(null) }

    BackHandler(enabled = count > 0) { model.clearSelection() }

    if (count > 0) {
        Surface(color = MaterialTheme.colorScheme.surfaceVariant, tonalElevation = 3.dp) {
            Row(
                Modifier.fillMaxWidth().padding(start = 4.dp, end = 8.dp, top = 2.dp, bottom = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(onClick = { model.clearSelection() }) {
                    Icon(Icons.Filled.Close, contentDescription = "Clear selection")
                }
                Text(
                    "$count selected",
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = Emerald,
                )
                Spacer(Modifier.weight(1f))
                if (count < total) {
                    IconButton(onClick = { model.selectAll() }) {
                        Icon(Icons.Filled.DoneAll, contentDescription = "Select all $total")
                    }
                }
                val remove = model.selectionRemove
                if (remove != null) {
                    IconButton(onClick = {
                        val tracks = model.selectedTracks()
                        tracks.forEach { remove(it.id) }
                        model.notify("Removed ${tracks.size} ${if (tracks.size == 1) "song" else "songs"}")
                        model.clearSelection()
                    }) {
                        Icon(
                            Icons.Filled.Delete,
                            contentDescription = "Remove from playlist",
                            tint = MaterialTheme.colorScheme.error,
                        )
                    }
                }
                IconButton(onClick = {
                    val tracks = model.selectedTracks()
                    model.playAll(tracks, 0)
                }) {
                    Icon(Icons.Filled.PlayArrow, contentDescription = "Play selected")
                }
                Button(
                    onClick = { adding = model.selectedTracks() },
                    contentPadding = ButtonDefaults.ButtonWithIconContentPadding,
                ) {
                    Icon(Icons.AutoMirrored.Filled.PlaylistAdd, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Add")
                }
            }
        }
    }

    adding?.let { tracks ->
        AddToPlaylistDialog(
            tracks = tracks,
            onDismiss = { adding = null },
            onAdded = {
                adding = null
                model.clearSelection()
            },
        )
    }
}
