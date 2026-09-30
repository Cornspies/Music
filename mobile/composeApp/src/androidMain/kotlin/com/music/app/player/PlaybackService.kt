package com.music.app.player

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import com.music.app.MainActivity
import com.music.app.widget.NowPlayingWidgetState

class PlaybackService : MediaSessionService() {

    private var session: MediaSession? = null

    @OptIn(UnstableApi::class)
    override fun onCreate() {
        super.onCreate()
        val exo = ExoPlayer.Builder(this)
            .setMediaSourceFactory(DefaultMediaSourceFactory(StreamCache.dataSourceFactory(this)))
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setContentType(C.AUDIO_CONTENT_TYPE_MUSIC)
                    .setUsage(C.USAGE_MEDIA)
                    .build(),
                true,
            )
            .setHandleAudioBecomingNoisy(true)
            // Keeps the CPU and Wi-Fi awake while streaming with the screen
            // off; ExoPlayer holds the locks only while playing.
            .setWakeMode(C.WAKE_MODE_NETWORK)
            .build()
        val player = SkipForwardingPlayer(exo)
        player.addListener(object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) = publishWidgetState(player)
            override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) = publishWidgetState(player)
            override fun onMediaMetadataChanged(mediaMetadata: androidx.media3.common.MediaMetadata) = publishWidgetState(player)
        })
        val openApp = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        session = MediaSession.Builder(this, player)
            .setSessionActivity(openApp)
            .build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = session

    override fun onTaskRemoved(rootIntent: Intent?) {
        val player = session?.player
        if (player == null || !player.playWhenReady || player.mediaItemCount == 0) {
            stopSelf()
        }
    }

    override fun onDestroy() {
        session?.run {
            player.release()
            release()
        }
        session = null
        NowPlayingWidgetState.clear(this)
        super.onDestroy()
    }

    private fun publishWidgetState(player: Player) {
        val md = player.mediaMetadata
        NowPlayingWidgetState.publish(
            this,
            title = md.title?.toString().orEmpty(),
            artist = md.artist?.toString().orEmpty(),
            playing = player.isPlaying,
            hasItem = player.mediaItemCount > 0,
        )
    }

    companion object {
        fun mediaButtonIntent(context: Context, keyCode: Int): Intent =
            Intent(Intent.ACTION_MEDIA_BUTTON)
                .setClass(context, PlaybackService::class.java)
                .putExtra(
                    Intent.EXTRA_KEY_EVENT,
                    android.view.KeyEvent(android.view.KeyEvent.ACTION_DOWN, keyCode),
                )
    }
}

class SkipForwardingPlayer(private val exo: ExoPlayer) : ForwardingPlayer(exo) {

    override fun getAvailableCommands(): Player.Commands =
        super.getAvailableCommands().buildUpon()
            .addAll(
                COMMAND_SEEK_TO_NEXT,
                COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
                COMMAND_SEEK_TO_PREVIOUS,
                COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
            )
            .build()

    override fun isCommandAvailable(command: Int): Boolean =
        when (command) {
            COMMAND_SEEK_TO_NEXT,
            COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
            COMMAND_SEEK_TO_PREVIOUS,
            COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
            -> true
            else -> super.isCommandAvailable(command)
        }

    override fun hasNextMediaItem(): Boolean = true
    override fun hasPreviousMediaItem(): Boolean = true

    override fun seekToNext() = skip(forward = true)
    override fun seekToNextMediaItem() = skip(forward = true)
    override fun seekToPrevious() = skip(forward = false)
    override fun seekToPreviousMediaItem() = skip(forward = false)

    private fun skip(forward: Boolean) {
        val handler = if (forward) PlayerController.skipForward else PlayerController.skipBack
        if (handler != null) {
            handler()
        } else if (!forward) {
            exo.seekTo(0)
        }
    }
}
