package com.music.app.ui.screens

import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.EmeraldDark
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc800
import kotlinx.coroutines.delay
import kotlin.time.Duration.Companion.milliseconds

/** Search tab — debounced full-library search (tracks, albums, artists, playlists). */
@Composable
fun SearchScreen() {
    val model = LocalAppModel.current

    var query by remember { mutableStateOf("") }
    val focusRequester = remember { FocusRequester() }

    // Debounce keystrokes (350 ms), then ask the server.
    LaunchedEffect(query) {
        val trimmed = query.trim()
        if (trimmed.isEmpty()) {
            model.clearSearch()
        } else {
            delay(350.milliseconds)
            if (query.trim() == trimmed) model.search(trimmed)
        }
    }
    LaunchedEffect(Unit) { focusRequester.requestFocus() }

    Column(
        Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        Spacer(Modifier.height(16.dp))
        OutlinedTextField(
            value = query,
            onValueChange = { query = it },
            modifier = Modifier
                .fillMaxWidth()
                .focusRequester(focusRequester),
            placeholder = { Text("Songs, artists, albums…") },
            leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
            trailingIcon = {
                if (query.isNotEmpty()) {
                    IconButton(onClick = { query = "" }) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Clear")
                    }
                }
            },
            singleLine = true,
        )
        Spacer(Modifier.height(12.dp))

        val results = model.searchResults
        if (query.isBlank()) {
            Text(
                "Search your library.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 32.dp, start = 8.dp),
            )
        } else if (results == null && !model.searchLoading) {
            // Nothing yet (debounce window).
        } else {
            LazyColumn(
                verticalArrangement = Arrangement.spacedBy(4.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
            ) {
                val tracks = results?.tracks.orEmpty()
                if (tracks.isNotEmpty()) {
                    item { SectionHeader("Tracks") }
                    items(tracks, key = { "t-${it.id}" }) { track ->
                        TrackRow(track = track, context = tracks)
                    }
                }
                val albums = results?.albums.orEmpty()
                if (albums.isNotEmpty()) {
                    item { SectionHeader("Albums") }
                    items(albums, key = { "a-${it.name}-${it.artist}" }) { album ->
                        ResultRow(
                            title = album.name,
                            subtitle = "${album.artist} • ${album.trackCount} tracks • ${formatTotalDuration(album.totalDuration)}",
                            coverTrackId = if (album.hasCover) album.coverTrackId else null,
                        )
                    }
                }
                val artists = results?.artists.orEmpty()
                if (artists.isNotEmpty()) {
                    item { SectionHeader("Artists") }
                    items(artists, key = { "r-${it.name}" }) { artist ->
                        ResultRow(
                            title = artist.name,
                            subtitle = "${artist.trackCount} tracks • ${formatTotalDuration(artist.totalDuration)}",
                            coverTrackId = null,
                        )
                    }
                }
                val playlists = results?.playlists.orEmpty()
                if (playlists.isNotEmpty()) {
                    item { SectionHeader("Playlists") }
                    items(playlists, key = { "p-${it.id}" }) { playlist ->
                        ResultRow(
                            title = playlist.name,
                            subtitle = "${playlist.trackCount} tracks • ${formatTotalDuration(playlist.totalDuration)}",
                            coverTrackId = null,
                        )
                    }
                }
                if (results != null &&
                    tracks.isEmpty() && albums.isEmpty() && artists.isEmpty() && playlists.isEmpty()
                ) {
                    item {
                        Column(Modifier.padding(top = 32.dp)) {
                            Text(
                                "No results",
                                style = MaterialTheme.typography.titleMedium,
                                fontWeight = FontWeight.SemiBold,
                            )
                            Text(
                                "Try a different search.",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SectionHeader(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.titleMedium,
        fontWeight = FontWeight.Bold,
        modifier = Modifier.padding(top = 16.dp, bottom = 8.dp),
    )
}

/** Compact result row for albums / artists / playlists. */
@Composable
private fun ResultRow(
    title: String,
    subtitle: String,
    coverTrackId: String?,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Reuse the track cover loader when the aggregate points at a cover track.
        val coverTrack = coverTrackId?.let {
            com.music.app.api.Track(id = it, title = "", hasCover = true)
        }
        if (coverTrack != null) {
            CoverImage(track = coverTrack, size = 44.dp)
        } else {
            Box(
                Modifier
                    .size(44.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(Brush.linearGradient(listOf(Zinc800, EmeraldDark.copy(alpha = 0.35f)))),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Filled.MusicNote, contentDescription = null, tint = Zinc500)
            }
        }
        Spacer(Modifier.padding(start = 12.dp))
        Column {
            Text(
                title,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
            )
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
            )
        }
    }
}
