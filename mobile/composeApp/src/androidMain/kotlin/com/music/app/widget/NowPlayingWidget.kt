package com.music.app.widget

import android.content.Context
import android.content.Intent
import android.view.KeyEvent
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.action.actionStartService
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import com.music.app.MainActivity
import com.music.app.player.PlaybackService
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import androidx.core.content.edit

class NowPlayingWidget : GlanceAppWidget() {

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val state = NowPlayingWidgetState.read(context)
        provideContent {
            GlanceTheme {
                WidgetContent(state)
            }
        }
    }

    @Composable
    private fun WidgetContent(state: NowPlayingWidgetState.Snapshot) {
        val bg = ColorProvider(Color(0xFF09090B))
        val fg = ColorProvider(Color(0xFFF4F4F5))
        val dim = ColorProvider(Color(0xFFA1A1AA))
        val accent = ColorProvider(Color(0xFF34D399))

        Column(
            modifier = GlanceModifier
                .fillMaxSize()
                .background(bg)
                .cornerRadius(20.dp)
                .padding(14.dp)
                .clickable(actionStartActivity<MainActivity>()),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (state.hasItem) state.title.ifBlank { "Untitled" } else "Music",
                style = TextStyle(color = fg, fontSize = 15.sp, fontWeight = FontWeight.Bold),
                maxLines = 1,
            )
            Text(
                if (state.hasItem) state.artist.ifBlank { "Unknown artist" } else "Nothing playing",
                style = TextStyle(color = dim, fontSize = 12.sp),
                maxLines = 1,
            )
            Spacer(GlanceModifier.height(10.dp))
            Row(modifier = GlanceModifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                ControlText("⏮", fg, KeyEvent.KEYCODE_MEDIA_PREVIOUS)
                Spacer(GlanceModifier.width(18.dp))
                ControlText(if (state.playing) "⏸" else "▶", accent, KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE)
                Spacer(GlanceModifier.width(18.dp))
                ControlText("⏭", fg, KeyEvent.KEYCODE_MEDIA_NEXT)
            }
        }
    }

    @Composable
    private fun ControlText(glyph: String, color: ColorProvider, keyCode: Int) {
        Text(
            glyph,
            style = TextStyle(color = color, fontSize = 22.sp),
            modifier = GlanceModifier
                .padding(horizontal = 6.dp)
                .clickable(actionStartService(mediaButton(keyCode), isForegroundService = true)),
        )
    }

    @Composable
    private fun mediaButton(keyCode: Int): Intent =
        PlaybackService.mediaButtonIntent(androidx.glance.LocalContext.current, keyCode)
}

class NowPlayingWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = NowPlayingWidget()
}

object NowPlayingWidgetState {
    data class Snapshot(val title: String, val artist: String, val playing: Boolean, val hasItem: Boolean)

    private const val PREFS = "music-widget"

    fun read(context: Context): Snapshot {
        val p = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return Snapshot(
            title = p.getString("title", "") ?: "",
            artist = p.getString("artist", "") ?: "",
            playing = p.getBoolean("playing", false),
            hasItem = p.getBoolean("hasItem", false),
        )
    }

    fun publish(context: Context, title: String, artist: String, playing: Boolean, hasItem: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            putString("title", title)
                .putString("artist", artist)
                .putBoolean("playing", playing)
                .putBoolean("hasItem", hasItem)
        }
        refresh(context)
    }

    fun clear(context: Context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit { clear() }
        refresh(context)
    }

    private fun refresh(context: Context) {
        val app = context.applicationContext
        CoroutineScope(Dispatchers.Default).launch {
            runCatching { NowPlayingWidget().updateAll(app) }
        }
    }
}
