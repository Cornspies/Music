package com.music.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
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
import com.music.app.ui.theme.Emerald

/**
 * A thin strip shown above any screen while the server is unreachable.
 *
 * The app deliberately keeps its normal screens in this state — the library,
 * playlists and favourites all still render, just filled with whatever is
 * downloaded. This bar exists so that's never confusing: it says why the lists
 * look shorter and offers a way back.
 *
 * Renders nothing when online, so screens can include it unconditionally.
 */
@Composable
fun OfflineBanner(modifier: Modifier = Modifier) {
    val model = LocalAppModel.current
    if (!model.offlineMode) return

    Row(
        modifier
            .fillMaxWidth()
            .background(Emerald.copy(alpha = 0.10f))
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            Icons.Filled.CloudOff,
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
                "Offline",
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.SemiBold,
            )
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
