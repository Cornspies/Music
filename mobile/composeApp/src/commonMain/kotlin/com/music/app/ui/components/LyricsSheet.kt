package com.music.app.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lyrics
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.api.LyricLine
import com.music.app.api.Track
import com.music.app.ui.theme.Zinc400
import com.music.app.ui.theme.Zinc500
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

/** A line is current from its own time until the next one starts. */
private fun activeIndex(lines: List<LyricLine>, position: Double): Int {
    var idx = -1
    for ((i, l) in lines.withIndex()) {
        if (l.t <= position + 0.15) idx = i else break
    }
    return idx
}

private fun sourceLabel(source: String): String = when (source) {
    "embedded" -> "From the file"
    "lrclib" -> "From LRCLIB"
    "manual" -> "Added by hand"
    else -> ""
}

/**
 * Lyrics for [track] as a bottom sheet over the player. Synced lyrics follow
 * playback — the current line is highlighted and kept centred, tapping a line
 * seeks to it — and plain ones are just text to read. Follows the track, so
 * skipping while the sheet is open swaps the words.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalTime::class)
@Composable
fun LyricsSheet(track: Track, onDismiss: () -> Unit) {
    val model = LocalAppModel.current
    val position by model.player.positionSec.collectAsState()
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)

    LaunchedEffect(track.id) { model.loadLyrics(track) }
    val lyrics = model.lyricsFor(track)
    val synced = lyrics?.synced?.takeIf { it.isNotEmpty() }
    val current = if (synced != null) activeIndex(synced, position) else -1

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheetState) {
        Column(
            Modifier
                .fillMaxWidth()
                .fillMaxHeight(0.85f)
                .padding(horizontal = 8.dp),
        ) {
            Text(
                track.title,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
                maxLines = 1,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            )
            Text(
                track.artist.ifBlank { "Unknown artist" },
                style = MaterialTheme.typography.bodySmall,
                color = Zinc400,
                textAlign = TextAlign.Center,
                maxLines = 1,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            )
            Spacer(Modifier.height(12.dp))

            Box(Modifier.weight(1f).fillMaxWidth()) {
                when {
                    lyrics == null -> Centered("Loading lyrics…")
                    !lyrics.found -> Centered("No lyrics found", icon = true)
                    lyrics.instrumental && lyrics.plain.isBlank() -> Centered("Instrumental")
                    synced != null -> SyncedLines(synced, current) { model.seekTo(it) }
                    else -> Text(
                        lyrics.plain,
                        style = MaterialTheme.typography.bodyLarge,
                        lineHeight = MaterialTheme.typography.bodyLarge.lineHeight * 1.3,
                        modifier = Modifier
                            .fillMaxSize()
                            .verticalScroll(rememberScrollState())
                            .padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
            }

            if (lyrics != null && lyrics.found) {
                Text(
                    sourceLabel(lyrics.source),
                    style = MaterialTheme.typography.labelSmall,
                    color = Zinc500,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            } else {
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

@OptIn(ExperimentalTime::class)
@Composable
private fun SyncedLines(lines: List<LyricLine>, current: Int, onSeek: (Double) -> Unit) {
    val listState = rememberLazyListState()

    // Follow playback, unless the reader is scrolling on their own: a manual
    // scroll pauses the auto-follow for a few seconds.
    var lastUserScroll by remember { mutableLongStateOf(0L) }
    var following by remember { mutableStateOf(false) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress }.collect { scrolling ->
            if (scrolling && !following) lastUserScroll = Clock.System.now().toEpochMilliseconds()
        }
    }
    LaunchedEffect(current) {
        if (current < 0) return@LaunchedEffect
        if (Clock.System.now().toEpochMilliseconds() - lastUserScroll < 4000) return@LaunchedEffect
        // Centre the line: scroll so it sits about a third of the way down.
        val viewport = listState.layoutInfo.viewportSize.height
        following = true
        try {
            listState.animateScrollToItem(current, scrollOffset = -viewport / 3)
        } finally {
            following = false
        }
    }

    LazyColumn(
        state = listState,
        contentPadding = PaddingValues(vertical = 120.dp, horizontal = 8.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(lines) { i, l ->
            Text(
                l.line.ifBlank { "♪" },
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                color = when {
                    i == current -> MaterialTheme.colorScheme.onBackground
                    i < current -> Zinc500
                    else -> Zinc400
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { onSeek(l.t) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun Centered(text: String, icon: Boolean = false) {
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
    ) {
        if (icon) {
            Icon(Icons.Filled.Lyrics, contentDescription = null, tint = Zinc500)
            Spacer(Modifier.height(8.dp))
        }
        Text(text, style = MaterialTheme.typography.bodyMedium, color = Zinc400)
    }
}
