package com.music.app.ui.components

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.music.app.api.Lyrics
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc400

@Composable
fun LyricsStrip(
    lyrics: Lyrics,
    position: Double,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val synced = lyrics.synced?.takeIf { it.isNotEmpty() }
    val (current, next) = if (synced != null) {
        var i = -1
        for ((k, l) in synced.withIndex()) {
            if (l.t <= position + 0.15) i = k else break
        }
        (synced.getOrNull(i)?.line?.ifBlank { "♪" } ?: "♪") to synced.getOrNull(i + 1)?.line
    } else {
        val lines = lyrics.plain.lines().filter { it.isNotBlank() }
        (lines.firstOrNull() ?: "") to lines.getOrNull(1)
    }

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
        AnimatedContent(
            targetState = current,
            transitionSpec = { fadeIn(tween(220)) togetherWith fadeOut(tween(160)) },
            label = "lyric line",
        ) { line ->
            Text(
                line,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        Text(
            next ?: "",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.55f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
