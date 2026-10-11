package com.music.app.ui.screens

import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import com.music.app.offline.DownloadState
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.ui.theme.Emerald
import kotlinx.coroutines.delay
import kotlin.time.Duration.Companion.milliseconds

@Composable
fun FavoritesScreen() {
    val model = LocalAppModel.current

    LaunchedEffect(model.favorites) {
        if (model.favorites == null) model.loadFavorites()
    }
    LaunchedEffect(Unit) {
        while (true) {
            delay(30_000.milliseconds)
            if (model.user != null) model.loadFavorites()
        }
    }

    Column(
        Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        Spacer(Modifier.height(24.dp))
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Filled.Favorite, contentDescription = null, tint = Emerald)
            Spacer(Modifier.padding(start = 8.dp))
            Text(
                "Favorites",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
            )
            Spacer(Modifier.weight(1f))
            val favorites = model.favorites
            if (!favorites.isNullOrEmpty()) {
                Button(onClick = { model.playAll(favorites, 0, playContext = "favorites") }) {
                    Text("Play all")
                }
                Spacer(Modifier.padding(start = 8.dp))
                TextButton(onClick = { model.playAll(favorites, playContext = "favorites", shuffled = true) }) {
                    Icon(Icons.Filled.Shuffle, contentDescription = null)
                    Spacer(Modifier.padding(start = 4.dp))
                    Text("Shuffle")
                }
            }
        }
        val favorites = model.favorites
        val total = favorites?.sumOf { it.duration } ?: 0.0
        if (!favorites.isNullOrEmpty()) {
            Row(
                Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "${favorites.size} tracks • ${formatTotalDuration(total)}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                FavoritesDownloadButton(favorites)
            }
            model.favoritesDownloadError?.let { message ->
                Text(
                    message,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
        Spacer(Modifier.height(12.dp))

        when {
            model.favoritesLoading && favorites == null -> LoadingBox()
            model.favoritesError != null && favorites == null ->
                ErrorState(model.favoritesError ?: "", onRetry = { model.loadFavorites() })
            favorites.isNullOrEmpty() ->
                EmptyState(
                    icon = Icons.Filled.Favorite,
                    title = "No favorites yet",
                )
            else -> RefreshableContent(
                isRefreshing = model.favoritesLoading,
                onRefresh = { model.loadFavorites() },
            ) {
                LazyColumn(
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
                ) {
                    items(favorites, key = { it.id }) { track ->
                        TrackRow(track = track, context = favorites, playContext = "favorites")
                    }
                }
            }
        }
    }
}

@Composable
private fun FavoritesDownloadButton(favorites: List<Track>) {
    val model = LocalAppModel.current
    val progress = model.favoritesDownload
    val allDownloaded = favorites.all { model.downloads.stateOf(it.id) == DownloadState.DONE }
    var confirmRemove by remember { mutableStateOf(false) }

    TextButton(
        enabled = progress != null || allDownloaded || !model.offlineMode,
        onClick = {
            when {
                progress != null -> model.stopFavoritesDownload()
                allDownloaded -> confirmRemove = true
                else -> model.downloadAllFavorites()
            }
        },
    ) {
        when {
            progress != null -> {
                val (done, total) = progress
                CircularProgressIndicator(
                    progress = { if (total > 0) done.toFloat() / total else 0f },
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = Emerald,
                )
                Spacer(Modifier.width(8.dp))
                Text("$done/$total")
                Spacer(Modifier.width(4.dp))
                Icon(Icons.Filled.Close, contentDescription = "Stop downloading", modifier = Modifier.size(18.dp))
            }
            allDownloaded -> {
                Icon(Icons.Filled.DownloadDone, contentDescription = null, tint = Emerald, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("Downloaded")
            }
            else -> {
                Icon(Icons.Filled.Download, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text("Download all")
            }
        }
    }

    if (confirmRemove) {
        AlertDialog(
            onDismissRequest = { confirmRemove = false },
            title = { Text("Downloaded favorites") },
            text = { Text("Tracks that are also in a downloaded playlist stay on this device.") },
            confirmButton = {
                TextButton(onClick = {
                    model.removeFavoriteDownloads()
                    confirmRemove = false
                }) { Text("Remove downloads", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { confirmRemove = false }) { Text("Cancel") }
            },
        )
    }
}
