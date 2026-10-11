package com.music.app.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Album
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.Track
import com.music.app.ui.albumArtistOf
import com.music.app.ui.artistKey
import com.music.app.ui.components.ArtistAvatar
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.MixCollage
import com.music.app.ui.components.SectionHeader
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.hasArtist
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc400
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

fun albumContext(name: String, artist: String): String =
    "album:" + buildJsonObject {
        put("name", name)
        put("artist", artist)
    }.toString()

@Composable
private fun CollectionLayout(
    eyebrow: String,
    title: String,
    subtitle: String,
    tracks: List<Track>,
    playContext: String?,
    art: @Composable () -> Unit,
    extra: (LazyListScope.() -> Unit)? = null,
) {
    val model = LocalAppModel.current
    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 4.dp, top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = { model.pop() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
        }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                Column(
                    Modifier.fillMaxWidth().padding(horizontal = 24.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    art()
                    Spacer(Modifier.height(16.dp))
                    Text(
                        eyebrow.uppercase(),
                        style = MaterialTheme.typography.labelSmall,
                        color = Emerald,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        title,
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Black,
                        textAlign = TextAlign.Center,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.bodySmall,
                        color = Zinc400,
                        textAlign = TextAlign.Center,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.height(14.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        Button(onClick = { model.playAll(tracks, 0, playContext = playContext) }, enabled = tracks.isNotEmpty()) {
                            Icon(Icons.Filled.PlayArrow, contentDescription = null)
                            Spacer(Modifier.width(4.dp))
                            Text("Play")
                        }
                        OutlinedButton(
                            onClick = { model.playAll(tracks, playContext = playContext, shuffled = true) },
                            enabled = tracks.isNotEmpty(),
                        ) {
                            Icon(Icons.Filled.Shuffle, contentDescription = null, modifier = Modifier.size(18.dp))
                            Spacer(Modifier.width(4.dp))
                            Text("Shuffle")
                        }
                    }
                    Spacer(Modifier.height(8.dp))
                }
            }
            extra?.invoke(this)
            if (tracks.isEmpty()) {
                item { EmptyState(Icons.Filled.MusicNote, "No tracks here") }
            } else {
                items(tracks, key = { it.id }) { track ->
                    TrackRow(track = track, context = tracks, playContext = playContext)
                }
            }
        }
    }
}

private fun summary(tracks: List<Track>): String =
    "${tracks.size} ${if (tracks.size == 1) "track" else "tracks"} · ${formatTotalDuration(tracks.sumOf { it.duration })}"

@Composable
fun MixScreen(screen: Screen.Mix) {
    val model = LocalAppModel.current
    val home = model.home
    LaunchedEffect(Unit) { if (home == null) model.loadHome() }
    val mix = home?.mixes?.firstOrNull { it.id == screen.id }
    val all = model.tracks
    val byId = remember(all) { all.orEmpty().associateBy { it.id } }
    if (home == null || all == null) {
        LoadingBox()
        return
    }
    if (mix == null) {
        EmptyState(Icons.Filled.Album, "This mix isn't around today")
        return
    }
    val tracks = mix.trackIds.mapNotNull { byId[it] }
    CollectionLayout(
        eyebrow = if (mix.kind == "artist") "Mix for you" else "Made for you",
        title = mix.title,
        subtitle = listOf(mix.subtitle, summary(tracks)).filter { it.isNotBlank() }.joinToString(" · "),
        tracks = tracks,
        playContext = "mix:${mix.id}",
        art = { MixCollage(mix, byId, Modifier.size(220.dp)) },
    )
}

@Composable
fun ArtistScreen(screen: Screen.Artist) {
    val model = LocalAppModel.current
    val all = model.tracks
    LaunchedEffect(Unit) { if (all == null) model.loadLibrary() }
    val tracks = remember(all, screen.name) { all.orEmpty().filter { hasArtist(it.artist, screen.name) } }
    val albums = remember(tracks) {
        tracks
            .filter { it.album.isNotBlank() && it.album != "Unknown Album" }
            .groupBy { it.album to albumArtistOf(it) }
            .map { (key, list) -> Triple(key.first, key.second, list.firstOrNull { it.hasCover } ?: list.first()) }
    }
    val home = model.home
    val picture = home?.topArtists?.firstOrNull { artistKey(it.name) == artistKey(screen.name) }?.picture
        ?: home?.suggestedArtists?.firstOrNull { artistKey(it.name) == artistKey(screen.name) }?.picture
    if (all == null) {
        LoadingBox()
        return
    }
    CollectionLayout(
        eyebrow = "Artist",
        title = screen.name,
        subtitle = summary(tracks) + if (albums.isNotEmpty()) " · ${albums.size} ${if (albums.size == 1) "album" else "albums"}" else "",
        tracks = tracks,
        playContext = "artist:${artistKey(screen.name)}",
        art = { ArtistAvatar(screen.name, 200.dp, picture = picture) },
        extra = if (albums.isEmpty()) null else {
            {
                item { SectionHeader("Albums") }
                item {
                    LazyRow(
                        contentPadding = PaddingValues(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        items(albums, key = { "${it.first}\n${it.second}" }) { (album, artist, cover) ->
                            Column(
                                Modifier
                                    .width(120.dp)
                                    .clip(RoundedCornerShape(10.dp))
                                    .clickable { model.openAlbum(album, artist) },
                            ) {
                                CoverImage(cover, 120.dp)
                                Spacer(Modifier.height(6.dp))
                                Text(album, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                        }
                    }
                }
                item { SectionHeader("Songs") }
            }
        },
    )
}

@Composable
fun AlbumScreen(screen: Screen.Album) {
    val model = LocalAppModel.current
    val all = model.tracks
    LaunchedEffect(Unit) { if (all == null) model.loadLibrary() }
    val tracks = remember(all, screen) {
        all.orEmpty()
            .filter { it.album == screen.name && albumArtistOf(it) == screen.artist }
            .sortedWith(compareBy({ it.trackNo.takeIf { n -> n > 0 } ?: Int.MAX_VALUE }, { it.title.lowercase() }))
    }
    if (all == null) {
        LoadingBox()
        return
    }
    val cover = tracks.firstOrNull { it.hasCover } ?: tracks.firstOrNull()
    CollectionLayout(
        eyebrow = "Album",
        title = screen.name,
        subtitle = listOfNotNull(
            screen.artist.ifBlank { "Unknown artist" },
            tracks.map { it.artist }.distinct().size.takeIf { it > 1 }?.let { "$it artists" },
            summary(tracks),
        ).joinToString(" · "),
        tracks = tracks,
        playContext = albumContext(screen.name, screen.artist),
        art = {
            if (cover != null) {
                CoverImage(cover, 220.dp, corner = 12.dp)
            } else {
                Icon(Icons.Filled.Album, contentDescription = null, modifier = Modifier.size(120.dp))
            }
        },
    )
}
