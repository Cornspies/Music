package com.music.app.player

import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import com.music.app.appContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.io.File

/**
 * Media3 ExoPlayer-backed implementation. Position is polled every 250 ms on the
 * main thread — plenty for a seek bar; isPlaying updates via Player.Listener.
 */
actual class PlayerController actual constructor() {

    private val mainHandler = Handler(Looper.getMainLooper())

    private val player: ExoPlayer = ExoPlayer.Builder(appContext).build()

    actual var onEnded: (() -> Unit)? = null

    private val _isPlaying = MutableStateFlow(false)
    actual val isPlaying: StateFlow<Boolean> = _isPlaying

    private val _positionSec = MutableStateFlow(0.0)
    actual val positionSec: StateFlow<Double> = _positionSec

    private val positionPoller = object : Runnable {
        override fun run() {
            if (player.playbackState == Player.STATE_READY || player.playbackState == Player.STATE_BUFFERING) {
                _positionSec.value = player.currentPosition / 1000.0
            }
            mainHandler.postDelayed(this, 250)
        }
    }

    init {
        player.addListener(object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                _isPlaying.value = isPlaying
            }

            override fun onPlaybackStateChanged(playbackState: Int) {
                if (playbackState == Player.STATE_ENDED) {
                    _isPlaying.value = false
                    _positionSec.value = 0.0
                    onEnded?.invoke()
                }
            }
        })
        mainHandler.post(positionPoller)
    }

    actual fun load(url: String, autoplay: Boolean) {
        _positionSec.value = 0.0
        // Offline playback passes a plain filesystem path; streaming passes an
        // http(s) URL. Uri.fromFile handles the escaping a raw "file://" + path
        // string would get wrong for names containing spaces or '#'.
        val item = if (url.startsWith("/")) {
            MediaItem.fromUri(Uri.fromFile(File(url)))
        } else {
            MediaItem.fromUri(url)
        }
        player.setMediaItem(item)
        player.prepare()
        player.playWhenReady = autoplay
        if (autoplay) player.play()
    }

    actual fun play() {
        player.play()
    }

    actual fun pause() {
        player.pause()
    }

    actual fun seekTo(sec: Double) {
        player.seekTo((sec * 1000.0).toLong())
        _positionSec.value = sec
    }

    actual fun release() {
        mainHandler.removeCallbacks(positionPoller)
        player.release()
    }
}
