package com.music.app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
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
                Button(onClick = { model.playAll(favorites, 0) }) {
                    Text("Play all")
                }
                Spacer(Modifier.padding(start = 8.dp))
                TextButton(onClick = {
                    model.shuffle = true
                    model.playAll(favorites.shuffled(), 0)
                    model.shuffle = false
                }) {
                    Icon(Icons.Filled.Shuffle, contentDescription = null)
                    Spacer(Modifier.padding(start = 4.dp))
                    Text("Shuffle")
                }
            }
        }
        val favorites = model.favorites
        val total = favorites?.sumOf { it.duration } ?: 0.0
        if (!favorites.isNullOrEmpty()) {
            Text(
                "${favorites.size} tracks • ${formatTotalDuration(total)}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
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
                        TrackRow(track = track, context = favorites)
                    }
                }
            }
        }
    }
}
