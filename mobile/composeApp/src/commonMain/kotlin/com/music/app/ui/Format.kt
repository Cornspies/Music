@file:OptIn(ExperimentalTime::class)

package com.music.app.ui

import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

/** "3:45" / "1:02:03" — track durations and seek-bar labels. */
fun formatDuration(totalSeconds: Double): String {
    val s = totalSeconds.coerceAtLeast(0.0).toLong()
    val h = s / 3600
    val m = (s % 3600) / 60
    val sec = s % 60
    val sec2 = sec.toString().padStart(2, '0')
    return if (h > 0) {
        "$h:" + m.toString().padStart(2, '0') + ":" + sec2
    } else {
        "$m:$sec2"
    }
}

/** "12h 34m" / "5m" — aggregate listening time (stats, playlists, albums). */
fun formatTotalDuration(seconds: Double): String {
    val s = seconds.coerceAtLeast(0.0).toLong()
    val h = s / 3600
    val m = (s % 3600) / 60
    return when {
        h > 0 -> "${h}h ${m}m"
        m > 0 -> "${m}m"
        s > 0 -> "${s}s"
        else -> "0m"
    }
}

/** "3.4 MB" / "812 KB" — file sizes (locale-independent, one decimal max). */
fun formatBytes(bytes: Long): String = when {
    bytes < 0L -> "—"
    bytes < 1_000_000L -> "${bytes / 1_000} KB"
    bytes < 1_000_000_000L -> {
        val mb = ((bytes / 1_000_000.0) * 10.0).toLong() / 10.0
        "$mb MB"
    }
    else -> {
        val gb = ((bytes / 1_000_000_000.0) * 10.0).toLong() / 10.0
        "$gb GB"
    }
}

/** "in 6 days", "in 3 hours", "in 20 minutes" — for expiry times; "expired" once past. */
fun formatTimeUntil(iso: String): String {
    val at = runCatching { Instant.parse(iso) }.getOrNull() ?: return ""
    val minutes = (at - Clock.System.now()).inWholeMinutes
    return when {
        minutes <= 0 -> "expired"
        minutes < 60 -> "in $minutes minute${if (minutes == 1L) "" else "s"}"
        minutes < 60 * 24 -> (minutes / 60).let { "in $it hour${if (it == 1L) "" else "s"}" }
        else -> (minutes / (60 * 24)).let { "in $it day${if (it == 1L) "" else "s"}" }
    }
}
