@file:OptIn(ExperimentalTime::class)

package com.music.app.ui

import com.music.app.api.Track
import kotlin.math.abs
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

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

fun formatSizeChange(bytes: Long): String = when {
    abs(bytes) < 1_000L -> "same size"
    bytes > 0 -> "+${formatBytes(bytes)}"
    else -> "−${formatBytes(-bytes)}"
}

private val codecNames = mapOf(
    "mp3" to "MP3", "aac" to "AAC", "alac" to "ALAC", "flac" to "FLAC",
    "opus" to "Opus", "vorbis" to "Vorbis", "wmav1" to "WMA", "wmav2" to "WMA", "wmapro" to "WMA",
)

private val mimeNames = mapOf(
    "audio/mpeg" to "MP3", "audio/mp4" to "AAC", "audio/aac" to "AAC", "audio/flac" to "FLAC",
    "audio/ogg" to "Ogg", "audio/opus" to "Opus", "audio/webm" to "WebM", "audio/wav" to "WAV",
    "audio/x-ms-wma" to "WMA",
)

fun codecName(track: Track): String {
    val codec = track.codec.takeIf { it != "unknown" }.orEmpty()
    return when {
        codec.startsWith("pcm_") -> "WAV"
        codec.isNotEmpty() -> codecNames[codec] ?: codec.uppercase()
        else -> mimeNames[track.mimeType] ?: track.mimeType.removePrefix("audio/").uppercase()
    }
}

fun qualityDetail(track: Track): String = buildList {
    add(codecName(track))
    if (track.bitrate > 0) add("${track.bitrate} kbps")
    if (track.sampleRate > 0) add("${formatKhz(track.sampleRate)} kHz")
    if (track.bitDepth > 0) add("${track.bitDepth}-bit")
    when (track.channels) {
        0 -> Unit
        1 -> add("mono")
        2 -> add("stereo")
        else -> add("${track.channels} ch")
    }
    if (track.fileSize > 0) add(formatBytes(track.fileSize))
}.joinToString(" · ")

private fun formatKhz(hz: Int): String =
    if (hz % 1000 == 0) "${hz / 1000}" else "${hz / 1000}.${(hz % 1000) / 100}"
