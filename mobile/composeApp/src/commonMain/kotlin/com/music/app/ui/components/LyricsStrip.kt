package com.music.app.ui.components

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.OpenInFull
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.music.app.LocalAppModel
import com.music.app.api.LyricLine
import com.music.app.api.Lyrics
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc400
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest

@Composable
fun LyricsStrip(
    lyrics: Lyrics,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val synced = lyrics.synced?.takeIf { it.isNotEmpty() }

    Column(
        modifier
            .fillMaxWidth()
            .height(92.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(Emerald.copy(alpha = 0.14f))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "LYRICS",
                style = MaterialTheme.typography.labelSmall,
                color = Emerald,
                letterSpacing = 1.2.sp,
            )
            Spacer(Modifier.weight(1f))
            Icon(
                Icons.Filled.OpenInFull,
                contentDescription = "Show all lyrics",
                tint = Zinc400,
                modifier = Modifier.size(14.dp),
            )
        }
        Spacer(Modifier.height(4.dp))
        Box(Modifier.fillMaxWidth().weight(1f).clipToBounds()) {
            if (synced != null) {
                SyncedLines(synced)
            } else {
                val lines = lyrics.plain.lines().filter { it.isNotBlank() }
                StripLines(lines.firstOrNull() ?: "", lines.getOrNull(1))
            }
        }
    }
}

// The two lines move up together when the song moves on, like a small version of the
// full lyrics view.
@Composable
private fun SyncedLines(synced: List<LyricLine>) {
    val player = LocalAppModel.current.player
    // Kept as a State and read only inside the effect, so a position tick doesn't redraw
    // the strip unless the line changes.
    val position = player.positionSec.collectAsStateWithLifecycle()
    var index by remember(synced) { mutableIntStateOf(activeIndex(synced, player.currentPositionSec())) }

    LaunchedEffect(synced) {
        snapshotFlow { position.value }.collectLatest {
            // Ticks come every 250 ms. Read the exact position now, and if the next line starts
            // before the following tick, wait for it, so a line doesn't show up to 250 ms late.
            val now = player.currentPositionSec()
            index = activeIndex(synced, now)
            val next = synced.getOrNull(index + 1) ?: return@collectLatest
            val wait = next.t - 0.15 - now
            if (wait > 0.3 || !player.isPlaying.value) return@collectLatest
            delay((wait * 1000).toLong() + 1)
            index = activeIndex(synced, player.currentPositionSec())
        }
    }

    AnimatedContent(
        targetState = index,
        transitionSpec = {
            // Seeking back moves the lines down instead.
            val up = if (targetState >= initialState) 1 else -1
            (slideInVertically(tween(320)) { up * it / 2 } + fadeIn(tween(320))) togetherWith
                (slideOutVertically(tween(320)) { -up * it / 2 } + fadeOut(tween(220)))
        },
        label = "lyric lines",
    ) { i ->
        StripLines(
            synced.getOrNull(i)?.line?.ifBlank { "♪" } ?: "♪",
            synced.getOrNull(i + 1)?.line,
        )
    }
}

@Composable
private fun StripLines(current: String, next: String?) {
    Column(Modifier.fillMaxWidth()) {
        Text(
            current,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.fillMaxWidth(),
        )
        Text(
            next ?: "",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.55f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
