@file:OptIn(ExperimentalTime::class)

package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.music.app.LocalAppModel
import com.music.app.localTimeLabel
import com.music.app.ui.theme.Emerald
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

@Composable
fun OfflineBanner(modifier: Modifier = Modifier) {
    val model = LocalAppModel.current
    if (!model.offlineMode) return
    val down = model.serverDown
    val title = when {
        down == null -> "Offline"
        down.maintenance -> "Down for maintenance"
        else -> "Can't reach the server"
    }
    val until = down?.until
    val detail = listOfNotNull(
        when {
            until == null -> null
            until > Clock.System.now() -> "Back around ${localTimeLabel(until.toEpochMilliseconds())}"
            else -> "Back any moment"
        },
        down?.note,
    ).joinToString("\n").ifEmpty { null }

    Row(
        modifier
            .fillMaxWidth()
            .background(Emerald.copy(alpha = 0.10f))
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (down?.maintenance == true) Icons.Filled.Build else Icons.Filled.CloudOff,
            contentDescription = null,
            tint = Emerald,
            modifier = Modifier.size(18.dp),
        )
        Column(
            Modifier
                .weight(1f)
                .padding(start = 10.dp),
        ) {
            Text(
                title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.SemiBold,
            )
            if (detail != null) {
                Text(
                    detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (model.reconnecting) {
            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = Emerald)
        } else {
            TextButton(onClick = { model.reconnect() }) {
                Text("Reconnect", color = Emerald, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}
