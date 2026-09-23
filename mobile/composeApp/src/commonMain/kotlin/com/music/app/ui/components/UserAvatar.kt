package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.material3.LocalTextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import kotlin.math.abs
import kotlin.math.max

private val AVATAR_COLORS = listOf(
    Color(0xFF34D399) to Color(0xFF0F766E),
    Color(0xFF60A5FA) to Color(0xFF1E3A8A),
    Color(0xFFF472B6) to Color(0xFF9D174D),
    Color(0xFFFBBF24) to Color(0xFF92400E),
    Color(0xFFA78BFA) to Color(0xFF5B21B6),
    Color(0xFF22D3EE) to Color(0xFF155E75),
)

private fun gradientFor(seed: String): Brush {
    val s = seed.ifEmpty { "music" }
    var h = 0
    for (c in s) h = h * 31 + c.code
    val (from, to) = AVATAR_COLORS[abs(h) % AVATAR_COLORS.size]
    return Brush.linearGradient(listOf(from, to))
}

private fun initialsOf(name: String): String {
    val parts = name.trim().split(" ", "_", "-", ".").filter { it.isNotBlank() }
    return when {
        parts.isEmpty() -> "?"
        parts.size == 1 -> parts[0].take(1).uppercase()
        else -> (parts[0].take(1) + parts[1].take(1)).uppercase()
    }
}

@Composable
fun UserAvatar(
    userId: String,
    username: String,
    hasAvatar: Boolean,
    size: Dp = 20.dp,
    modifier: Modifier = Modifier,
    label: String = username,
) {
    val model = LocalAppModel.current
    var failed by remember(userId, hasAvatar) { mutableStateOf(false) }

    Box(
        modifier
            .size(size)
            .clip(CircleShape)
            .background(gradientFor(username.ifEmpty { userId }))
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = initialsOf(username.ifEmpty { "?" }),
            color = Color.White,
            fontWeight = FontWeight.Bold,
            fontSize = max(9f, size.value * 0.52f).sp,
            lineHeight = max(9f, size.value * 0.52f).sp,
            style = LocalTextStyle.current.copy(
                lineHeightStyle = LineHeightStyle(
                    alignment = LineHeightStyle.Alignment.Center,
                    trim = LineHeightStyle.Trim.Both,
                ),
            ),
            textAlign = TextAlign.Center,
        )
        if (hasAvatar && !failed) {
            AsyncImage(
                model = model.api.avatarUrl(userId),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                onError = { failed = true },
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}
