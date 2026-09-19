package com.music.app.player

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import platform.AVFoundation.AVPlayer
import platform.AVFoundation.AVPlayerItem
import platform.AVFoundation.AVPlayerItemDidPlayToEndTimeNotification
import platform.AVFoundation.AVPlayerTimeControlStatusPlaying
// AVPlayer's playback controls are Objective-C category members, which
// Kotlin/Native exposes as extensions that need importing by name.
import platform.AVFoundation.currentTime
import platform.AVFoundation.pause
import platform.AVFoundation.play
import platform.AVFoundation.replaceCurrentItemWithPlayerItem
import platform.AVFoundation.seekToTime
import platform.AVFoundation.timeControlStatus
import platform.CoreMedia.CMTimeGetSeconds
import platform.CoreMedia.CMTimeMakeWithSeconds
import platform.Foundation.NSDefaultRunLoopMode
import platform.Foundation.NSNotificationCenter
import platform.Foundation.NSRunLoop
import platform.Foundation.NSTimer
import platform.Foundation.NSURL
import platform.darwin.NSObjectProtocol

/**
 * AVFoundation AVPlayer-backed implementation. Position is polled with an
 * NSTimer every 250 ms on the main run loop; end-of-track arrives via the
 * AVPlayerItemDidPlayToEndTime notification.
 */
// CMTime is a C struct, so every call that passes one through cinterop is
// behind the foreign-API opt-in.
@OptIn(ExperimentalForeignApi::class)
actual class PlayerController actual constructor() {

    actual var onEnded: (() -> Unit)? = null

    private val _isPlaying = MutableStateFlow(false)
    actual val isPlaying: StateFlow<Boolean> = _isPlaying

    private val _positionSec = MutableStateFlow(0.0)
    actual val positionSec: StateFlow<Double> = _positionSec

    private var player: AVPlayer? = null

    private val endObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVPlayerItemDidPlayToEndTimeNotification, `object` = null, queue = null) { _ ->
            _isPlaying.value = false
            onEnded?.invoke()
        }

    private val poller: NSTimer = NSTimer.timerWithTimeInterval(0.25, repeats = true) { _ ->
        val p = player
        if (p != null) {
            _isPlaying.value = p.timeControlStatus == AVPlayerTimeControlStatusPlaying
            val seconds = CMTimeGetSeconds(p.currentTime())
            if (!seconds.isNaN()) _positionSec.value = seconds
        }
    }.also { timer ->
        NSRunLoop.mainRunLoop.addTimer(timer, NSDefaultRunLoopMode)
    }

    actual fun load(url: String, autoplay: Boolean) {
        _positionSec.value = 0.0
        // Offline playback passes a plain filesystem path; streaming passes an
        // http(s) URL. fileURLWithPath does the percent-encoding that
        // URLWithString would silently fail on for paths with spaces.
        val nsUrl = if (url.startsWith("/")) NSURL.fileURLWithPath(url) else NSURL.URLWithString(url)
        if (nsUrl == null) return
        val item = AVPlayerItem(uRL = nsUrl)
        val existing = player
        existing?.replaceCurrentItemWithPlayerItem(null)
        player = AVPlayer(playerItem = item)
        if (autoplay) player?.play()
    }

    actual fun play() {
        player?.play()
    }

    actual fun pause() {
        player?.pause()
    }

    actual fun seekTo(sec: Double) {
        val p = player ?: return
        val time = CMTimeMakeWithSeconds(sec, 600)
        p.seekToTime(time)
        _positionSec.value = sec
    }

    actual fun release() {
        poller.invalidate()
        NSNotificationCenter.defaultCenter.removeObserver(endObserver)
        player?.pause()
        player?.replaceCurrentItemWithPlayerItem(null)
        player = null
    }
}
