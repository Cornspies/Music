package com.music.app.player

import android.content.ComponentName
import android.net.Uri
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.MoreExecutors
import com.music.app.appContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.io.File
import androidx.core.net.toUri

actual class PlayerController actual constructor() {

    private var controllerFuture: ListenableFuture<MediaController>? = null
    private var controller: MediaController? = null
    private val pending = ArrayDeque<(MediaController) -> Unit>()
    private var released = false

    actual var onEnded: (() -> Unit)? = null
    actual var onNext: (() -> Unit)? = null
        set(value) {
            field = value
            skipForward = value
        }
    actual var onPrevious: (() -> Unit)? = null
        set(value) {
            field = value
            skipBack = value
        }

    private val _isPlaying = MutableStateFlow(false)
    actual val isPlaying: StateFlow<Boolean> = _isPlaying

    private val _positionSec = MutableStateFlow(0.0)
    actual val positionSec: StateFlow<Double> = _positionSec

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    // Ended counts too: listening is measured up to where the track stopped.
    private fun readPosition(): Double? {
        val c = controller ?: return null
        return if (c.playbackState != Player.STATE_IDLE) c.currentPosition / 1000.0 else null
    }

    actual fun currentPositionSec(): Double = readPosition() ?: _positionSec.value

    // What the app last asked for, until the controller is connected to say.
    private var playRequested = false

    actual fun wantsToPlay(): Boolean = controller?.playWhenReady ?: playRequested

    private val listener = object : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            _isPlaying.value = isPlaying
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            if (playbackState == Player.STATE_ENDED) {
                _isPlaying.value = false
                onEnded?.invoke()
            }
        }
    }

    init {
        val token = SessionToken(appContext, ComponentName(appContext, PlaybackService::class.java))
        val future = MediaController.Builder(appContext, token).buildAsync()
        controllerFuture = future
        future.addListener({
            if (released) {
                runCatching { future.get().release() }
                return@addListener
            }
            val c = runCatching { future.get() }.getOrNull() ?: return@addListener
            controller = c
            c.addListener(listener)
            _isPlaying.value = c.isPlaying
            while (pending.isNotEmpty()) pending.removeFirst()(c)
        }, MoreExecutors.directExecutor())
        scope.tickPosition(_positionSec, _isPlaying, ::readPosition)
    }

    private fun withController(block: (MediaController) -> Unit) {
        val c = controller
        if (c != null) block(c) else pending.addLast(block)
    }

    actual fun load(url: String, autoplay: Boolean, info: NowPlayingInfo?) {
        _positionSec.value = 0.0
        val uri = if (url.startsWith("/")) Uri.fromFile(File(url)) else url.toUri()
        val metadata = MediaMetadata.Builder().apply {
            if (info != null) {
                setTitle(info.title)
                setArtist(info.artist)
                setAlbumTitle(info.album.ifBlank { null })
                setArtworkUri(info.artworkUrl?.let(Uri::parse))
                if (info.durationSec > 0) setDurationMs((info.durationSec * 1000).toLong())
            }
        }.build()
        val item = MediaItem.Builder().setUri(uri).setMediaId(url).setMediaMetadata(metadata).build()
        playRequested = autoplay
        withController { c ->
            c.setMediaItem(item)
            c.prepare()
            c.playWhenReady = autoplay
            if (autoplay) c.play()
        }
    }

    actual fun play() {
        playRequested = true
        withController { it.play() }
    }

    actual fun pause() {
        playRequested = false
        withController { it.pause() }
    }

    actual fun seekTo(sec: Double) {
        withController { it.seekTo((sec * 1000.0).toLong()) }
        _positionSec.value = sec
    }

    actual fun release() {
        released = true
        scope.cancel()
        skipForward = null
        skipBack = null
        controller?.removeListener(listener)
        controller?.release()
        controller = null
        controllerFuture?.let { MediaController.releaseFuture(it) }
        controllerFuture = null
    }

    companion object {
        @Volatile internal var skipForward: (() -> Unit)? = null
        @Volatile internal var skipBack: (() -> Unit)? = null
    }
}
