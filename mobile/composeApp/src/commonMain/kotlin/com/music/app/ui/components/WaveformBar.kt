package com.music.app.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import com.music.app.ui.theme.Emerald

@Composable
fun WaveformBar(
    peaks: List<Int>,
    fraction: Float,
    modifier: Modifier = Modifier,
    playedColor: Color = Emerald,
    restColor: Color = Color.White.copy(alpha = 0.22f),
) {
    if (peaks.isEmpty()) return
    Canvas(modifier) {
        val n = peaks.size
        val slot = size.width / n
        val barWidth = (slot * 0.62f).coerceAtLeast(1f)
        val playedUntil = size.width * fraction
        for (i in 0 until n) {
            val h = (peaks[i].coerceIn(3, 100) / 100f) * size.height
            val x = i * slot + (slot - barWidth) / 2f
            val y = (size.height - h) / 2f
            drawRoundRect(
                color = if (x + barWidth / 2f <= playedUntil) playedColor else restColor,
                topLeft = Offset(x, y),
                size = Size(barWidth, h),
                cornerRadius = androidx.compose.ui.geometry.CornerRadius(barWidth / 2f),
            )
        }
    }
}
