package com.music.app.player

import android.content.Context
import android.content.Intent
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import com.music.app.widget.NowPlayingWidgetState

/**
 * Foreground service that owns the ExoPlayer.
 *
 * Media3 turns the session into a media-style notification, lock-screen
 * controls, Bluetooth/headset buttons and Android Auto for free. Playback
 * keeps going when the activity is destroyed; the process stays alive as long
 * as something is playing and the service stops itself once it's paused and
 * the app is swiped away.
 *
 * The queue stays in the app (AppModel). The player only ever holds one item,
 * so a plain ExoPlayer would advertise no next/previous commands and the
 * notification would show none. [SkipForwardingPlayer] claims them and hands
 * the presses to whichever [PlayerController] is connected.
 */
class PlaybackService : MediaSessionService() {

    private var session: MediaSession? = null

    override fun onCreate() {
        super.onCreate()
        val exo = ExoPlayer.Builder(this)
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setContentType(C.AUDIO_CONTENT_TYPE_MUSIC)
                    .setUsage(C.USAGE_MEDIA)
                    .build(),
                /* handleAudioFocus = */ true,
            )
            .setHandleAudioBecomingNoisy(true)
            .build()
        val player = SkipForwardingPlayer(exo)
        player.addListener(object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) = publishWidgetState(player)
            override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) = publishWidgetState(player)
            override fun onMediaMetadataChanged(mediaMetadata: androidx.media3.common.MediaMetadata) = publishWidgetState(player)
        })
        session = MediaSession.Builder(this, player).build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = session

    override fun onTaskRemoved(rootIntent: Intent?) {
        val player = session?.player
        if (player == null || !player.playWhenReady || player.mediaItemCount == 0) {
            // Nothing audible: don't linger as a foreground service.
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
        /** Media-button intent the home-screen widget sends to this service. */
        fun mediaButtonIntent(context: Context, keyCode: Int): Intent =
            Intent(Intent.ACTION_MEDIA_BUTTON)
                .setClass(context, PlaybackService::class.java)
                .putExtra(
                    Intent.EXTRA_KEY_EVENT,
                    android.view.KeyEvent(android.view.KeyEvent.ACTION_DOWN, keyCode),
                )
    }
}

/**
 * Advertises next/previous even with a single item loaded and forwards the
 * presses to the app, which owns the queue. Everything else is ExoPlayer.
 */
class SkipForwardingPlayer(private val exo: ExoPlayer) : ForwardingPlayer(exo) {

    override fun getAvailableCommands(): Player.Commands =
        super.getAvailableCommands().buildUpon()
            .addAll(
                Player.COMMAND_SEEK_TO_NEXT,
                Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
                Player.COMMAND_SEEK_TO_PREVIOUS,
                Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
            )
            .build()

    override fun isCommandAvailable(command: Int): Boolean =
        when (command) {
            Player.COMMAND_SEEK_TO_NEXT,
            Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
            Player.COMMAND_SEEK_TO_PREVIOUS,
            Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
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
            // No app connected to decide: behave like a normal "previous".
            exo.seekTo(0)
        }
    }
}
