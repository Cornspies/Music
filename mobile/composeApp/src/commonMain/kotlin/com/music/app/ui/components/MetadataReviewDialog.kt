package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.BrokenImage
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Image
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import kotlin.time.Duration.Companion.milliseconds
import kotlinx.coroutines.delay
import androidx.compose.material3.FilterChip
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlin.time.Instant
import kotlin.time.ExperimentalTime
import kotlin.time.Clock
import coil3.compose.AsyncImage
import com.music.app.LocalAppModel
import com.music.app.api.MetadataCandidate
import com.music.app.api.Track
import com.music.app.api.friendlyError
import com.music.app.ui.formatDuration
import com.music.app.ui.theme.Emerald
import com.music.app.ui.theme.Zinc500
import com.music.app.ui.theme.Zinc700
import com.music.app.ui.theme.Zinc800
import com.music.app.ui.theme.Zinc950
import kotlinx.coroutines.launch

// Per-provider accent, mirroring the web app's source badges.
private val SourceSky = Color(0xFF7DD3FC)
private val SourceViolet = Color(0xFFC4B5FD)
private val SourcePink = Color(0xFFF9A8D4)
private val SourceRed = Color(0xFFFCA5A5)

private fun sourceColor(source: String): Color = when (source) {
    "MusicBrainz" -> SourceSky
    "Deezer" -> SourceViolet
    "iTunes" -> SourcePink
    "YouTube" -> SourceRed
    else -> Zinc500
}

private fun confidenceText(confidence: Double): String = when {
    confidence >= 0.8 -> "Strong match"
    confidence >= 0.5 -> "Likely match"
    else -> "Weak match"
}

private fun confidenceColor(confidence: Double): Color = when {
    confidence >= 0.8 -> Emerald
    confidence >= 0.5 -> Color(0xFFFBBF24) // amber-400
    else -> Zinc500
}

/** "Artist · Album · 2001 · 3:24", skipping whatever's missing. */
private fun candidateSubtitle(candidate: MetadataCandidate): String {
    val parts = mutableListOf<String>()
    parts.add(candidate.artist.ifBlank { "Unknown artist" })
    if (candidate.album.isNotBlank()) parts.add(candidate.album)
    if (candidate.year > 0) parts.add(candidate.year.toString())
    if (candidate.duration > 0.0) parts.add(formatDuration(candidate.duration))
    return parts.joinToString(" · ")
}

private fun trackSubtitleLine(track: Track): String {
    val parts = mutableListOf<String>()
    parts.add(track.artist.ifBlank { "Unknown artist" })
    if (track.album.isNotBlank()) parts.add(track.album)
    if (track.duration > 0.0) parts.add(formatDuration(track.duration))
    return parts.joinToString(" · ")
}

/**
 * Lets the user confirm (or correct) the metadata detected for freshly uploaded tracks.
 *
 * Walks [tracks] one at a time, showing every online match the server found across
 * MusicBrainz / Deezer / iTunes / YouTube, best first. Picking one overwrites that
 * track's tags and cover art; "Keep current" moves on without changing anything.
 * Dismisses itself once the last track is resolved or skipped.
 */
@Composable
fun MetadataReviewDialog(
    tracks: List<Track>,
    onDismiss: () -> Unit,
) {
    if (tracks.isEmpty()) return

    val model = LocalAppModel.current
    val scope = rememberCoroutineScope()

    var index by remember { mutableStateOf(0) }
    var candidates by remember { mutableStateOf<List<MetadataCandidate>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var lookupDisabled by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var applyingId by remember { mutableStateOf<String?>(null) }
    // Queue state from the last response; polled while pending.
    var pending by remember { mutableStateOf(false) }
    var queuePosition by remember { mutableStateOf(0) }
    var attempts by remember { mutableStateOf(0) }
    var retryAt by remember { mutableStateOf<String?>(null) }
    var partial by remember { mutableStateOf(false) }

    // trackId -> chosen candidate id, so revisiting a track shows what was applied.
    var resolved by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    // Bumped to force a reload when the user retries after an error.
    var reloadKey by remember { mutableStateOf(0) }

    val track = tracks.getOrNull(index) ?: return
    // Provider chip; null shows every source. Resets with the track.
    var sourceFilter by remember(track.id) { mutableStateOf<String?>(null) }

    LaunchedEffect(track.id, reloadKey) {
        loading = true
        error = null
        candidates = emptyList()
        pending = false
        // First answer, then — while the server's queue hasn't been through
        // this track — ask again every few seconds. The list grows as
        // providers answer and the position counts down.
        var first = true
        while (true) {
            try {
                val response = model.api.metadataCandidates(track.id)
                candidates = response.candidates
                lookupDisabled = response.lookupDisabled
                pending = response.pending
                queuePosition = response.queuePosition
                attempts = response.attempts
                retryAt = response.retryAt
                partial = response.partial
                error = null
            } catch (e: Exception) {
                if (first) error = friendlyError(e)
            } finally {
                loading = false
                first = false
            }
            if (!pending) break
            delay(2500.milliseconds)
        }
    }

    fun goNext() {
        if (index < tracks.lastIndex) index += 1 else onDismiss()
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Column {
                Text("Confirm track details", fontWeight = FontWeight.Bold)
            }
        },
        text = {
            Column(Modifier.fillMaxWidth()) {
                // What the track currently says, for comparison against the suggestions.
                Column(
                    Modifier
                        .fillMaxWidth()
                        .border(1.dp, Zinc700, RoundedCornerShape(8.dp))
                        .background(Zinc950, RoundedCornerShape(8.dp))
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    Text(
                        "CURRENTLY SAVED AS",
                        style = MaterialTheme.typography.labelSmall,
                        color = Zinc500,
                    )
                    Text(
                        track.title,
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Medium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        trackSubtitleLine(track),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }

                Spacer(Modifier.height(12.dp))

                if (!loading && error == null && pending) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp, color = Emerald)
                        Spacer(Modifier.width(8.dp))
                        Text(
                            buildString {
                                append(
                                    when {
                                        retryAt != null -> "A provider didn't answer — trying again ${formatIn(retryAt!!)} (attempt ${attempts + 1})"
                                        queuePosition > 1 -> "Waiting for the lookup queue · ${queuePosition - 1} ahead"
                                        attempts > 0 -> "A provider was rate-limited — retrying (attempt ${attempts + 1})"
                                        else -> "Searching MusicBrainz, Deezer, iTunes…"
                                    },
                                )
                                if (candidates.isNotEmpty()) append(" · ${candidates.size} found so far")
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Spacer(Modifier.height(8.dp))
                } else if (!loading && error == null && partial) {
                    Text(
                        "Not every provider answered; “Look up again” tries them all once more.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.tertiary,
                    )
                    Spacer(Modifier.height(8.dp))
                }

                val sources = candidates.map { it.source }.distinct()
                if (!loading && error == null && sources.size > 1) {
                    Row(
                        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        FilterChip(
                            selected = sourceFilter == null,
                            onClick = { sourceFilter = null },
                            label = { Text("All · ${candidates.size}") },
                        )
                        sources.forEach { s ->
                            FilterChip(
                                selected = sourceFilter == s,
                                onClick = { sourceFilter = s },
                                label = { Text("$s · ${candidates.count { it.source == s }}") },
                            )
                        }
                    }
                    Spacer(Modifier.height(8.dp))
                }
                val shown = if (sourceFilter == null) candidates else candidates.filter { it.source == sourceFilter }

                when {
                    loading -> LoadingBox(Modifier.heightIn(max = 140.dp))

                    error != null -> Column(
                        Modifier.fillMaxWidth(),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text(
                            error ?: "",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                        )
                        Spacer(Modifier.height(8.dp))
                        OutlinedButton(onClick = { reloadKey += 1 }) { Text("Try again") }
                    }

                    lookupDisabled -> Text(
                        "Online metadata lookup is disabled on this server.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )

                    candidates.isEmpty() && pending -> Spacer(Modifier.height(4.dp))

                    candidates.isEmpty() -> Text(
                        "No online matches found for this track.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )

                    else -> LazyColumn(
                        Modifier
                            .fillMaxWidth()
                            .heightIn(max = 340.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(shown, key = { it.id }) { candidate ->
                            CandidateRow(
                                trackId = track.id,
                                candidate = candidate,
                                isApplied = resolved[track.id] == candidate.id,
                                showAutoBadge = candidate.applied && resolved[track.id] == null,
                                busy = applyingId == candidate.id,
                                enabled = applyingId == null,
                                onApply = {
                                    applyingId = candidate.id
                                    scope.launch {
                                        try {
                                            val updated = model.api.applyMetadataCandidate(track.id, candidate.id)
                                            // If this track is downloaded, update the offline
                                            // copy's metadata too so it doesn't show stale tags.
                                            model.downloads.refreshMetadata(updated)
                                            resolved = resolved + (track.id to candidate.id)
                                            model.loadLibrary()
                                            model.loadStats()
                                            goNext()
                                        } catch (e: Exception) {
                                            error = friendlyError(e)
                                        } finally {
                                            applyingId = null
                                        }
                                    }
                                },
                            )
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { goNext() }, enabled = applyingId == null) {
                Text(if (index < tracks.lastIndex) "Keep current" else "Done")
            }
        },
        dismissButton = {
            Row {
                if (index > 0) {
                    TextButton(onClick = { index -= 1 }, enabled = applyingId == null) {
                        Text("Previous")
                    }
                }
                if (!lookupDisabled) {
                    TextButton(
                        onClick = {
                            scope.launch {
                                try {
                                    model.api.requestMetadataLookup(track.id)
                                    resolved = resolved - track.id
                                    reloadKey += 1
                                } catch (e: Exception) {
                                    error = friendlyError(e)
                                }
                            }
                        },
                        enabled = applyingId == null && !loading && !pending,
                    ) {
                        Text("Look up again")
                    }
                }
            }
        },
    )
}

@Composable
private fun CandidateRow(
    trackId: String,
    candidate: MetadataCandidate,
    isApplied: Boolean,
    showAutoBadge: Boolean,
    busy: Boolean,
    enabled: Boolean,
    onApply: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .border(
                1.dp,
                if (isApplied) Emerald else Zinc700,
                RoundedCornerShape(8.dp),
            )
            .background(Zinc950, RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CandidateThumb(trackId, candidate)
        Spacer(Modifier.padding(start = 12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Badge(candidate.source, sourceColor(candidate.source))
                if (showAutoBadge) {
                    Spacer(Modifier.padding(start = 6.dp))
                    Badge("Applied automatically", Emerald)
                }
            }
            Spacer(Modifier.height(4.dp))
            Text(
                candidate.title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                candidateSubtitle(candidate),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                confidenceText(candidate.confidence) +
                    " · " + (candidate.confidence * 100).toInt() + "%",
                style = MaterialTheme.typography.labelSmall,
                color = confidenceColor(candidate.confidence),
            )
        }
        Spacer(Modifier.padding(start = 8.dp))
        when {
            busy -> CircularProgressIndicator(
                modifier = Modifier.height(20.dp),
                strokeWidth = 2.dp,
            )
            isApplied -> Icon(
                Icons.Filled.Check,
                contentDescription = "Applied",
                tint = Emerald,
            )
            else -> OutlinedButton(onClick = onApply, enabled = enabled) {
                Text("Use this", style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}

/**
 * Thumbnail of a suggestion's cover art, so the right release can be told
 * apart at a glance. Fetched lazily through the server. Two distinct
 * fallbacks, because they mean different things to the person choosing: a
 * candidate that simply has no artwork shows a neutral image icon, while one
 * that *should* have artwork but couldn't be fetched (the server answers 404
 * for an expired list or an unreachable provider) shows a broken-image icon
 * — the art may still arrive when the candidate is applied.
 */
@Composable
private fun CandidateThumb(trackId: String, candidate: MetadataCandidate) {
    val model = LocalAppModel.current
    var failed by remember(candidate.id) { mutableStateOf(false) }
    Box(
        Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(6.dp))
            .background(Zinc800)
            .then(
                if (candidate.hasArtwork && failed) {
                    Modifier.border(1.dp, Zinc700, RoundedCornerShape(6.dp))
                } else {
                    Modifier
                },
            ),
        contentAlignment = Alignment.Center,
    ) {
        when {
            !candidate.hasArtwork -> Icon(
                Icons.Filled.Image,
                contentDescription = "No artwork available",
                tint = Zinc700,
                modifier = Modifier.size(20.dp),
            )
            failed -> Icon(
                Icons.Filled.BrokenImage,
                contentDescription = "Artwork couldn't be loaded",
                tint = Zinc500,
                modifier = Modifier.size(20.dp),
            )
            else -> AsyncImage(
                model = model.api.candidateArtworkUrl(trackId, candidate.id),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                onError = { failed = true },
                modifier = Modifier.size(48.dp),
            )
        }
    }
}

/** Small pill label used for the provider name and the auto-applied marker. */
@Composable
private fun Badge(text: String, color: Color) {
    Box(
        Modifier
            .border(1.dp, color.copy(alpha = 0.4f), RoundedCornerShape(4.dp))
            .background(color.copy(alpha = 0.12f), RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    ) {
        Text(
            text,
            style = MaterialTheme.typography.labelSmall,
            color = color,
            maxLines = 1,
        )
    }
}

/** "in 10 min", "in 12 h", "in a day" — when a parked retry runs. */
@OptIn(ExperimentalTime::class)
private fun formatIn(iso: String): String {
    val at = runCatching { Instant.parse(iso) }.getOrNull() ?: return "later"
    val minutes = (at - Clock.System.now()).inWholeMinutes
    return when {
        minutes < 1 -> "any moment"
        minutes < 60 -> "in $minutes min"
        minutes < 60 * 23 -> "in ${(minutes + 30) / 60} h"
        else -> "in a day"
    }
}
