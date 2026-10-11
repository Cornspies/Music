package com.music.app.ui.screens

import androidx.compose.material3.FilterChip
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.music.app.ui.components.ArtistCircle
import com.music.app.ui.components.MixCard
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudUpload
import androidx.compose.material.icons.filled.DownloadForOffline
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.Stats
import com.music.app.api.Track
import com.music.app.ui.components.AvatarButton
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.SectionHeader
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc400

@Composable
fun HomeScreen() {
    val model = LocalAppModel.current
    LaunchedEffect(Unit) {
        model.loadStats()
        model.loadHome()
        if (model.tracks == null) model.loadLibrary()
    }

    val stats = model.stats
    val home = model.home
    val allTracks = model.tracks
    val byId = remember(allTracks) { allTracks.orEmpty().associateBy { it.id } }
    var span by remember { mutableStateOf<String?>(null) }

    RefreshableContent(
        isRefreshing = model.tracksLoading || model.statsLoading,
        onRefresh = {
            model.refreshAll()
            model.loadHome()
        },
    ) {
    LazyColumn(
        Modifier.fillMaxWidth(),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        item {
            Row(
                Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        "Home",
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Bold,
                    )
                    model.user?.let {
                        Text(
                            it.username,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                IconButton(onClick = { model.push(Screen.Downloads) }) {
                    Icon(Icons.Filled.DownloadForOffline, contentDescription = "Downloads")
                }
                IconButton(onClick = { model.push(Screen.Upload) }) {
                    Icon(Icons.Filled.CloudUpload, contentDescription = "Upload music")
                }
                AvatarButton()
            }
        }

        if (home != null && home.mixes.isNotEmpty()) {
            item { SectionHeader("Mixes for you") }
            item {
                LazyRow(
                    contentPadding = PaddingValues(horizontal = 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    items(home.mixes, key = { it.id }) { mix ->
                        MixCard(mix, byId) { model.push(Screen.Mix(mix.id)) }
                    }
                }
            }
        }

        if (stats != null && stats.recentlyPlayed.isNotEmpty()) {
            item { SectionHeader("Recently played") }
            item { RecentlyAddedRow(stats.recentlyPlayed.take(10)) }
        }

        if (home != null) {
            val week = home.similar.week
            val month = home.similar.month
            val chosen = span ?: if (week.trackIds.isNotEmpty()) "week" else "month"
            val similar = (if (chosen == "week") week else month).let { s -> s.trackIds.mapNotNull { byId[it] } to s.basedOn }
            if (similar.first.isNotEmpty()) {
                item {
                    SectionHeader(
                        if (chosen == "week") "Like what you played this week" else "Like what you played this month",
                        trailing = if (week.trackIds.isNotEmpty() && month.trackIds.isNotEmpty()) {
                            {
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    FilterChip(selected = chosen == "week", onClick = { span = "week" }, label = { Text("Week") })
                                    FilterChip(selected = chosen == "month", onClick = { span = "month" }, label = { Text("Month") })
                                }
                            }
                        } else {
                            null
                        },
                    )
                }
                if (similar.second.isNotEmpty()) {
                    item {
                        Text(
                            "Based on " + similar.second.joinToString(", "),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 6.dp),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                item { RecentlyAddedRow(similar.first) }
            }

            if (home.topArtists.isNotEmpty()) {
                item { SectionHeader("Your top artists") }
                item {
                    LazyRow(contentPadding = PaddingValues(horizontal = 10.dp)) {
                        items(home.topArtists, key = { it.name }) { a ->
                            ArtistCircle(
                                name = a.name,
                                caption = if (a.listens > 0) "${a.listens} plays lately" else "${a.trackCount} songs",
                                picture = a.picture,
                            ) { model.openArtist(a.name) }
                        }
                    }
                }
            }

            if (home.suggestedArtists.isNotEmpty()) {
                item { SectionHeader("Artists you might like") }
                item {
                    LazyRow(contentPadding = PaddingValues(horizontal = 10.dp)) {
                        items(home.suggestedArtists, key = { it.name }) { a ->
                            ArtistCircle(
                                name = a.name,
                                caption = if (a.trackCount > 0) "${a.trackCount} in your library" else "Because you like ${a.because}",
                                picture = a.picture,
                            ) { model.openSuggestion(a) }
                        }
                    }
                }
            }
        }

        val tracks = model.tracks
        when {
            tracks == null && model.tracksLoading -> item { LoadingBox() }
            tracks != null && tracks.isEmpty() -> item {
                EmptyState(
                    Icons.Filled.MusicNote,
                    "No music yet",
                )
            }
            !tracks.isNullOrEmpty() -> {
                item { SectionHeader("Recently added") }
                item { RecentlyAddedRow(tracks.take(10)) }
            }
        }

        if (stats != null) {
            if (stats.mostPlayed.isEmpty()) {
                item {
                    SectionHeader("Most played")
                    EmptyState(
                        Icons.Filled.MusicNote,
                        "Nothing played yet",
                    )
                }
            } else {
                item { SectionHeader("Most played") }
                items(stats.mostPlayed, key = { it.id }) { track ->
                    TrackRow(track, stats.mostPlayed)
                }
            }
        }

        when {
            stats != null -> item { StatsHeader(stats) }
            model.statsLoading -> item { LoadingBox() }
            else -> item {
                ErrorState(model.statsError ?: "Could not load stats") { model.loadStats() }
            }
        }
    }
    }
}

@Composable
private fun StatsHeader(stats: Stats) {
    Surface(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        shape = RoundedCornerShape(20.dp),
        color = MaterialTheme.colorScheme.surface,
    ) {
        Column(Modifier.padding(16.dp)) {
            Row(Modifier.fillMaxWidth()) {
                Column(Modifier.weight(1f)) {
                    Text(
                        "${stats.trackCount}",
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Bold,
                        color = Emerald,
                    )
                    Text("tracks", style = MaterialTheme.typography.bodySmall, color = Zinc400)
                }
                Column(
                    Modifier.weight(1f),
                    horizontalAlignment = Alignment.End,
                ) {
                    Text(
                        formatTotalDuration(stats.totalDuration),
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Bold,
                    )
                    Text("of music", style = MaterialTheme.typography.bodySmall, color = Zinc400)
                }
            }
            Spacer(Modifier.height(16.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                StatChip("${stats.artistCount}", "artists")
                StatChip("${stats.albumCount}", "albums")
                StatChip("${stats.favoriteCount}", "favorites")
                StatChip("${stats.playlistCount}", "playlists")
            }
        }
    }
}

@Composable
private fun StatChip(value: String, label: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(value, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        Text(label, style = MaterialTheme.typography.bodySmall, color = Zinc400)
    }
}

@Composable
private fun RecentlyAddedRow(tracks: List<Track>) {
    val model = LocalAppModel.current
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(tracks, key = { it.id }) { track ->
            Column(
                Modifier
                    .width(132.dp)
                    .clickable { model.playTrack(track, tracks) },
            ) {
                CoverImage(
                    track,
                    Modifier
                        .fillMaxWidth()
                        .aspectRatio(1f),
                    corner = 12.dp,
                    displaySize = 132.dp,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    track.title,
                    style = MaterialTheme.typography.bodyMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    track.artist.ifBlank { "Unknown artist" },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}
