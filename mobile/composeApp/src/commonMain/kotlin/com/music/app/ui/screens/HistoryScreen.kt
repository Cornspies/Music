@file:OptIn(ExperimentalTime::class)

package com.music.app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.History
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import com.music.app.api.HistoryResponse
import com.music.app.ui.components.EmptyState
import com.music.app.ui.components.ErrorState
import com.music.app.ui.components.LoadingBox
import com.music.app.ui.components.RefreshableContent
import com.music.app.ui.components.TrackRow
import com.music.app.ui.formatTotalDuration
import com.music.app.api.friendlyError
import com.music.app.ui.theme.Emerald
import kotlin.time.Clock
import kotlin.time.Duration.Companion.days
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

/** The time windows the history can be narrowed to; null means everything. */
private enum class HistoryRange(val label: String, val days: Int?) {
    DAY("Today", 1),
    WEEK("This week", 7),
    MONTH("This month", 30),
    ALL("All time", null),
}

/**
 * Listening history, newest first, straight from the server so it covers every
 * device. Opened from the profile.
 */
@Composable
fun HistoryScreen() {
    val model = LocalAppModel.current

    var range by remember { mutableStateOf(HistoryRange.WEEK) }
    // The result remembers the range it answers, so "loading" is simply "what's
    // on screen isn't for the range that's selected".
    var result by remember { mutableStateOf<Triple<HistoryRange, HistoryResponse?, String?>?>(null) }
    var reloadTick by remember { mutableStateOf(0) }
    val loading = result?.first != range

    LaunchedEffect(range, reloadTick) {
        val since = range.days?.let { (Clock.System.now() - it.days).toString() }
        result = try {
            Triple(range, model.api.history(since = since), null)
        } catch (e: Exception) {
            Triple(range, null, friendlyError(e))
        }
    }

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
            Icon(Icons.Filled.History, contentDescription = null, tint = Emerald)
            Spacer(Modifier.width(8.dp))
            Text(
                "History",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }

        Row(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            HistoryRange.entries.forEach { r ->
                FilterChip(selected = range == r, onClick = { range = r }, label = { Text(r.label) })
            }
        }

        val data = result?.second
        if (data != null && data.playCount > 0) {
            Text(
                "${formatTotalDuration(data.listenedSeconds)} listened • ${data.playCount} plays • ${data.trackCount} tracks",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        Spacer(Modifier.height(8.dp))

        val error = result?.third
        when {
            data == null && error != null -> ErrorState(error, onRetry = { reloadTick++ })
            data == null -> LoadingBox()
            data.events.isEmpty() -> EmptyState(icon = Icons.Filled.History, title = "Nothing played yet")
            else -> RefreshableContent(
                isRefreshing = loading,
                onRefresh = { reloadTick++ },
            ) {
                val queue = data.events.map { it.track }
                LazyColumn(
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                    contentPadding = PaddingValues(bottom = 24.dp),
                ) {
                    itemsIndexed(data.events, key = { i, ev -> "${ev.playedAt}-${ev.track.id}-$i" }) { _, ev ->
                        TrackRow(track = ev.track, context = queue, showActions = false) {
                            Text(
                                formatPlayedAt(ev.playedAt),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
    }
}

/** "just now", "5 min ago", "3 h ago", "2 d ago", else the date. */
private fun formatPlayedAt(iso: String): String {
    val then = runCatching { Instant.parse(iso) }.getOrNull() ?: return iso.take(10)
    val minutes = (Clock.System.now() - then).inWholeMinutes
    return when {
        minutes < 1 -> "just now"
        minutes < 60 -> "$minutes min ago"
        minutes < 60 * 24 -> "${minutes / 60} h ago"
        minutes < 60 * 24 * 7 -> "${minutes / (60 * 24)} d ago"
        else -> iso.take(10)
    }
}
