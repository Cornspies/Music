package com.music.app.ui.components

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.offline.DownloadQuality
import kotlinx.coroutines.launch

// Download preferences, shown on the profile screen. Kept together because
// they all govern what happens when a download is started, not the list of
// what's already on the device.

/**
 * Wi-Fi-only downloads and the "keep offline" sync status. Playlists marked
 * "keep offline" on their own screen are re-synced on every launch; this is
 * where that can be triggered by hand and where the last run is reported.
 */
@Composable
fun SyncSettings() {
    val model = LocalAppModel.current
    val downloads = model.downloads
    val scope = rememberCoroutineScope()

    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Column(Modifier.weight(1f)) {
                Text(
                    "Download on Wi-Fi only",
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    if (downloads.waitingForWifi) "On mobile data now — downloads are paused." else "Playlists kept offline still sync their track list on any connection.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Switch(checked = downloads.wifiOnly, onCheckedChange = { downloads.updateWifiOnly(it) })
        }
        Spacer(Modifier.height(4.dp))
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            Text(
                when {
                    downloads.syncing -> "Syncing kept playlists…"
                    downloads.lastSyncedAt != null -> "Last synced ${downloads.lastSyncedAt}"
                    else -> "Mark a playlist “Keep offline” to sync it here automatically."
                },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            TextButton(
                enabled = !downloads.syncing && !model.offlineMode,
                onClick = { scope.launch { downloads.syncAll() } },
            ) { Text("Sync now") }
        }
    }
}

/**
 * Which version to keep for the handful of tracks stored twice.
 *
 * Shown here rather than on a settings screen of its own: it affects nothing
 * but downloads, and this is where downloads live. Tracks with only one stored
 * version — most of the library — ignore it entirely, which the caption says
 * plainly so the setting doesn't look broken.
 */
@Composable
fun DownloadQualitySetting() {
    val model = LocalAppModel.current
    val current = model.downloads.downloadQuality

    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
        Text(
            "Download preference",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(6.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            FilterChip(
                selected = current == DownloadQuality.ORIGINAL,
                onClick = { model.downloads.updateDownloadQuality(DownloadQuality.ORIGINAL) },
                label = { Text("Original quality") },
            )
            Spacer(Modifier.padding(start = 8.dp))
            FilterChip(
                selected = current == DownloadQuality.SMALLER,
                onClick = { model.downloads.updateDownloadQuality(DownloadQuality.SMALLER) },
                label = { Text("Smaller file") },
            )
        }
    }
}
