package com.music.app.ui.screens

import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import com.music.app.Screen
import com.music.app.api.DiscoverPlaylist
import com.music.app.api.DiscoverResult
import com.music.app.api.PlaylistImport
import com.music.app.api.friendlyError
import com.music.app.ui.theme.Emerald
import com.music.app.ui.components.CoverImage
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatDuration
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.EmeraldDark
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc800
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.time.Duration.Companion.milliseconds

@Composable
fun SearchScreen() {
    val model = LocalAppModel.current

    var query by remember { mutableStateOf("") }
    var source by remember { mutableStateOf<String?>(null) }
    val focusRequester = remember { FocusRequester() }

    LaunchedEffect(Unit) { model.loadDiscoverSources() }
    val sources = model.discoverSources.orEmpty()

    LaunchedEffect(query, source) {
        val trimmed = query.trim()
        val src = source
        if (trimmed.isEmpty()) {
            model.clearSearch()
            model.clearDiscover()
        } else {
            delay(350.milliseconds)
            if (query.trim() == trimmed) {
                if (src == null) model.search(trimmed) else model.discoverSearch(src, trimmed)
            }
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
            placeholder = {
                val catalogue = sources.firstOrNull { it.key == source }
                Text(
                    when {
                        source == null -> "Songs, artists, albums…"
                        catalogue?.links == true -> "Search ${catalogue.name}, or paste a link"
                        else -> "Search ${catalogue?.name ?: source}"
                    },
                )
            },
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
        if (sources.isNotEmpty()) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(selected = source == null, onClick = { source = null }, label = { Text("Library") })
                sources.forEach { s ->
                    FilterChip(selected = source == s.key, onClick = { source = s.key }, label = { Text(s.name) })
                }
            }
        }
        Spacer(Modifier.height(12.dp))

        if (source != null) {
            DiscoverResults(source!!, query)
            return@Column
        }

        val results = model.searchResults
        if (query.isBlank()) {
            Text(
                "Search your library.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 32.dp, start = 8.dp),
            )
        } else if (results == null && !model.searchLoading) {} else {
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
private fun DiscoverResults(source: String, query: String) {
    val model = LocalAppModel.current
    val results = model.discoverResults
    val error = model.discoverError
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { model.loadImports() }
    val imports = model.shownImports
    var starting by remember { mutableStateOf(false) }
    var importError by remember { mutableStateOf<String?>(null) }

    LazyColumn(
        verticalArrangement = Arrangement.spacedBy(4.dp),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
    ) {
        items(imports, key = { "import-${it.id}" }) { job ->
            ImportCard(
                job,
                onOpen = { model.push(Screen.PlaylistDetail(it)) },
                onCancel = { model.cancelImport(job.id) },
                onDismiss = { model.dismissImport(job.id) },
            )
        }
        when {
            query.isBlank() -> {}
            error != null && results == null -> item {
                Text(
                    error,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(top = 32.dp, start = 8.dp),
                )
            }
            results == null -> if (model.discoverLoading) {
                item {
                    Box(Modifier.fillMaxWidth().padding(top = 48.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                }
            }
            else -> {
                results.playlist?.let { pl ->
                    item(key = "playlist-${pl.id}") {
                        PlaylistBanner(
                            pl,
                            importing = imports.any { it.active && it.listId == pl.id },
                            starting = starting,
                            error = importError,
                            onImport = {
                                scope.launch {
                                    starting = true
                                    importError = null
                                    try {
                                        model.startImport(source, query.trim())
                                    } catch (e: Exception) {
                                        importError = friendlyError(e)
                                    } finally {
                                        starting = false
                                    }
                                }
                            },
                        )
                    }
                }
                if (results.results.isEmpty()) {
                    item {
                        Text(
                            "No results",
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.padding(top = 32.dp, start = 8.dp),
                        )
                    }
                }
                items(results.results, key = { "${it.source}-${it.id}" }) { result ->
                    DiscoverRow(source, result)
                }
                if (results.hasMore) {
                    item {
                        Box(Modifier.fillMaxWidth().padding(vertical = 8.dp), contentAlignment = Alignment.Center) {
                            if (model.discoverLoading) {
                                CircularProgressIndicator(Modifier.size(24.dp))
                            } else {
                                TextButton(onClick = { model.discoverLoadMore() }) { Text("Load more") }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PlaylistBanner(
    playlist: DiscoverPlaylist,
    importing: Boolean,
    starting: Boolean,
    error: String?,
    onImport: () -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(Emerald.copy(alpha = 0.12f))
            .padding(horizontal = 14.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    playlist.title,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                )
                Text(
                    buildString {
                        append("${playlist.count} tracks")
                        playlist.total?.let { append(" · the first ${playlist.count} of $it") }
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.width(8.dp))
            Button(onClick = onImport, enabled = !starting && !importing) {
                if (starting) {
                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                } else {
                    Text(if (importing) "Importing…" else "Import all")
                }
            }
        }
        error?.let {
            Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error, maxLines = 2)
        }
    }
}

@Composable
private fun ImportCard(job: PlaylistImport, onOpen: (String) -> Unit, onCancel: () -> Unit, onDismiss: () -> Unit) {
    var showFailures by remember(job.id) { mutableStateOf(false) }
    var showExisting by remember(job.id) { mutableStateOf(false) }
    var showDuplicates by remember(job.id) { mutableStateOf(false) }
    val model = LocalAppModel.current
    val parts = buildList {
        if (job.added > 0) add("${job.added} added")
        if (job.possibleDuplicates.isNotEmpty()) add("${job.possibleDuplicates.size} possible duplicates")
        if (job.existing > 0) add("${job.existing} already in the library")
        if (job.failed > 0) add("${job.failed} failed")
    }
    val status = when (job.state) {
        "queued" -> "Waiting for another import to finish"
        "listing" -> "Reading the playlist…"
        "running" -> job.current?.let { "Fetching “$it”" } ?: "Fetching…"
        "done" -> parts.ifEmpty { listOf("Nothing to add") }.joinToString(" · ")
        "cancelled" -> (listOf("Stopped") + parts).joinToString(" · ")
        else -> job.error ?: "Failed"
    }
    Column(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(Zinc800.copy(alpha = 0.6f))
            .padding(horizontal = 14.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    job.title.ifBlank { "Reading the playlist…" },
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                )
                Text(
                    (if (job.total > 0) "${job.done} of ${job.total} · " else "") + status,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (job.state == "failed") MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                )
            }
            if (job.active) {
                TextButton(onClick = onCancel) { Text("Cancel") }
            } else {
                IconButton(onClick = onDismiss) {
                    Icon(Icons.Filled.Close, contentDescription = "Dismiss")
                }
            }
        }
        if (job.active && job.total > 0) {
            LinearProgressIndicator(
                progress = { job.done.toFloat() / job.total },
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
                color = Emerald,
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            job.playlistId?.let { id ->
                TextButton(onClick = { onOpen(id) }, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                    Text("Open playlist", color = Emerald)
                }
                Spacer(Modifier.width(12.dp))
            }
            if (job.failures.isNotEmpty()) {
                TextButton(onClick = { showFailures = !showFailures }, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                    Text(if (showFailures) "Hide what couldn't be added" else "Show what couldn't be added (${job.failures.size})")
                }
            }
        }
        if (job.possibleDuplicates.isNotEmpty()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = { showDuplicates = !showDuplicates }, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                    Text(if (showDuplicates) "Hide the ${job.possibleDuplicates.size} possible duplicates" else "Show the ${job.possibleDuplicates.size} possible duplicates")
                }
                Spacer(Modifier.width(12.dp))
                TextButton(onClick = { model.push(Screen.Duplicates) }, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                    Text("Review", color = Emerald)
                }
            }
            if (showDuplicates) {
                job.possibleDuplicates.forEach { d ->
                    Text(
                        "${d.title} — looks like “${d.existingTitle}”" + (if (d.existingArtist.isNotBlank()) " by ${d.existingArtist}" else ""),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                    )
                }
            }
        }
        if (job.existingTracks.isNotEmpty()) {
            TextButton(onClick = { showExisting = !showExisting }, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                Text(if (showExisting) "Hide the ${job.existingTracks.size} already in the library" else "Show the ${job.existingTracks.size} already in the library")
            }
            if (showExisting) {
                job.existingTracks.forEach { t ->
                    Text(
                        if (t.reason.isBlank()) t.title else "${t.title} — ${t.reason}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                model.tracks?.firstOrNull { it.id == t.trackId }?.let { model.playTrack(it, listOf(it)) }
                            }
                            .padding(vertical = 2.dp),
                    )
                }
            }
        }
        if (showFailures) {
            job.failures.forEach { f ->
                Text(
                    "${f.title} — ${f.reason}",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                )
            }
        }
    }
}

@Composable
private fun DiscoverRow(source: String, result: DiscoverResult) {
    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()
    var adding by remember(result.id) { mutableStateOf(false) }
    var error by remember(result.id) { mutableStateOf<String?>(null) }
    val inLibrary = result.trackId != null

    Row(
        Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(8.dp))
                .background(Zinc800),
        ) {
            if (result.coverUrl != null) {
                AsyncImage(
                    model = model.api.discoverArtworkUrl(source, result.coverUrl),
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(
                result.title,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
            )
            Text(
                listOfNotNull(
                    result.artist.ifBlank { null },
                    result.license?.let { licenseLabel(it) },
                    result.genre,
                    result.year?.toString(),
                    result.duration?.takeIf { it > 0 }?.let { formatDuration(it) },
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
            )
            error?.let {
                Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error, maxLines = 2)
            }
        }
        Spacer(Modifier.width(8.dp))
        when {
            inLibrary -> TextButton(onClick = {
                model.tracks?.firstOrNull { it.id == result.trackId }?.let { model.playTrack(it, listOf(it)) }
            }) {
                Icon(Icons.Filled.Check, contentDescription = null, tint = Emerald, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(4.dp))
                Text("In library", color = Emerald)
            }
            adding -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            else -> Button(onClick = {
                adding = true
                error = null
                scope.launch {
                    try {
                        model.discoverAdd(result)
                    } catch (e: Exception) {
                        error = friendlyError(e)
                    } finally {
                        adding = false
                    }
                }
            }) { Text("Add") }
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

private fun licenseLabel(license: String): String = when {
    license == "no-rights-reserved" -> "Public domain"
    license.startsWith("cc-") -> "CC " + license.removePrefix("cc-").uppercase()
    else -> license
}
