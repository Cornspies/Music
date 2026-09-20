package com.music.app.player

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.useContents
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import platform.AVFAudio.AVAudioSession
import platform.AVFAudio.AVAudioSessionCategoryPlayback
import platform.AVFAudio.AVAudioSessionInterruptionNotification
import platform.AVFAudio.AVAudioSessionInterruptionOptionKey
import platform.AVFAudio.AVAudioSessionInterruptionOptionShouldResume
import platform.AVFAudio.AVAudioSessionInterruptionTypeBegan
import platform.AVFAudio.AVAudioSessionInterruptionTypeEnded
import platform.AVFAudio.AVAudioSessionInterruptionTypeKey
import platform.AVFAudio.setActive
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
import platform.CoreGraphics.CGSizeMake
import platform.CoreMedia.CMTimeGetSeconds
import platform.CoreMedia.CMTimeMakeWithSeconds
import platform.Foundation.NSData
import platform.Foundation.NSDefaultRunLoopMode
import platform.Foundation.NSNotificationCenter
import platform.Foundation.NSNumber
import platform.Foundation.NSOperationQueue
import platform.Foundation.NSRunLoop
import platform.Foundation.NSTimer
import platform.Foundation.NSURL
import platform.Foundation.NSURLSession
import platform.Foundation.NSUserDefaults
import platform.Foundation.dataTaskWithURL
import platform.MediaPlayer.MPChangePlaybackPositionCommandEvent
import platform.MediaPlayer.MPMediaItemArtwork
import platform.MediaPlayer.MPMediaItemPropertyAlbumTitle
import platform.MediaPlayer.MPMediaItemPropertyArtist
import platform.MediaPlayer.MPMediaItemPropertyArtwork
import platform.MediaPlayer.MPMediaItemPropertyPlaybackDuration
import platform.MediaPlayer.MPMediaItemPropertyTitle
import platform.MediaPlayer.MPNowPlayingInfoCenter
import platform.MediaPlayer.MPNowPlayingInfoPropertyElapsedPlaybackTime
import platform.MediaPlayer.MPNowPlayingInfoPropertyPlaybackRate
import platform.MediaPlayer.MPRemoteCommandCenter
import platform.MediaPlayer.MPRemoteCommandHandlerStatusCommandFailed
import platform.MediaPlayer.MPRemoteCommandHandlerStatusSuccess
import platform.UIKit.UIImage
import platform.darwin.NSObjectProtocol

/**
 * AVFoundation AVPlayer-backed implementation. Position is polled with an
 * NSTimer every 250 ms on the main run loop; end-of-track arrives via the
 * AVPlayerItemDidPlayToEndTime notification.
 *
 * The audio session is set to playback (so audio continues in the background
 * and through the mute switch), what's playing is published to
 * MPNowPlayingInfoCenter for the lock screen and Control Centre, and the
 * remote command centre feeds headset / lock-screen presses back in.
 */
// CMTime is a C struct, so every call that passes one through cinterop is
// behind the foreign-API opt-in.
@OptIn(ExperimentalForeignApi::class)
actual class PlayerController actual constructor() {

    actual var onEnded: (() -> Unit)? = null
    actual var onNext: (() -> Unit)? = null
    actual var onPrevious: (() -> Unit)? = null

    private val _isPlaying = MutableStateFlow(false)
    actual val isPlaying: StateFlow<Boolean> = _isPlaying

    private val _positionSec = MutableStateFlow(0.0)
    actual val positionSec: StateFlow<Double> = _positionSec

    private var player: AVPlayer? = null

    /** What the lock screen is currently told; updated as position changes. */
    private var nowPlaying: MutableMap<Any?, Any?> = mutableMapOf()
    private var currentArtworkUrl: String? = null

    private val endObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVPlayerItemDidPlayToEndTimeNotification, `object` = null, queue = null) { _ ->
            _isPlaying.value = false
            onEnded?.invoke()
        }

    /**
     * A phone call, alarm or Siri takes the audio session away and iOS pauses
     * the player for us; what it doesn't do is start it again. When the
     * interruption ends and the system says resuming is appropriate (it isn't
     * after, say, the user started music in another app), pick up where we
     * were — the same thing ExoPlayer's audio-focus handling does on Android.
     */
    private var wasPlayingBeforeInterruption = false
    private val interruptionObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVAudioSessionInterruptionNotification, `object` = null, queue = NSOperationQueue.mainQueue) { note ->
            val info = note?.userInfo ?: return@addObserverForName
            val type = (info[AVAudioSessionInterruptionTypeKey] as? NSNumber)?.unsignedLongValue
                ?: return@addObserverForName
            when (type) {
                AVAudioSessionInterruptionTypeBegan -> {
                    wasPlayingBeforeInterruption = _isPlaying.value
                }
                AVAudioSessionInterruptionTypeEnded -> {
                    val options = (info[AVAudioSessionInterruptionOptionKey] as? NSNumber)?.unsignedLongValue ?: 0uL
                    val shouldResume = options and AVAudioSessionInterruptionOptionShouldResume != 0uL
                    if (wasPlayingBeforeInterruption && shouldResume) {
                        configureAudioSession() // the session was deactivated by the interruption
                        play()
                    }
                    wasPlayingBeforeInterruption = false
                }
            }
        }

    private val poller: NSTimer = NSTimer.timerWithTimeInterval(0.25, repeats = true) { _ ->
        val p = player
        if (p != null) {
            val playing = p.timeControlStatus == AVPlayerTimeControlStatusPlaying
            if (playing != _isPlaying.value) {
                _isPlaying.value = playing
                publishNowPlaying(rate = if (playing) 1.0 else 0.0)
            }
            val seconds = CMTimeGetSeconds(p.currentTime())
            if (!seconds.isNaN()) _positionSec.value = seconds
        }
    }.also { timer ->
        NSRunLoop.mainRunLoop.addTimer(timer, NSDefaultRunLoopMode)
    }

    init {
        configureAudioSession()
        wireRemoteCommands()
    }

    actual fun load(url: String, autoplay: Boolean, info: NowPlayingInfo?) {
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
        if (info != null) setNowPlaying(info)
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
        publishNowPlaying(rate = if (_isPlaying.value) 1.0 else 0.0)
    }

    actual fun release() {
        poller.invalidate()
        NSNotificationCenter.defaultCenter.removeObserver(endObserver)
        NSNotificationCenter.defaultCenter.removeObserver(interruptionObserver)
        player?.pause()
        player?.replaceCurrentItemWithPlayerItem(null)
        player = null
        MPNowPlayingInfoCenter.defaultCenter().nowPlayingInfo = null
        clearWidgetState()
    }

    // ---- audio session / now playing ---------------------------------------

    private fun configureAudioSession() {
        val session = AVAudioSession.sharedInstance()
        runCatching {
            session.setCategory(AVAudioSessionCategoryPlayback, error = null)
            session.setActive(true, error = null)
        }
    }

    private fun setNowPlaying(info: NowPlayingInfo) {
        nowPlaying = mutableMapOf(
            MPMediaItemPropertyTitle to info.title,
            MPMediaItemPropertyArtist to info.artist,
            MPMediaItemPropertyAlbumTitle to info.album,
            MPMediaItemPropertyPlaybackDuration to info.durationSec,
            MPNowPlayingInfoPropertyElapsedPlaybackTime to 0.0,
            MPNowPlayingInfoPropertyPlaybackRate to 0.0,
        )
        publishNowPlaying(rate = 0.0)
        publishWidgetState(info)
        currentArtworkUrl = info.artworkUrl
        info.artworkUrl?.let { loadArtwork(it) }
    }

    private fun publishNowPlaying(rate: Double) {
        if (nowPlaying.isEmpty()) return
        nowPlaying[MPNowPlayingInfoPropertyElapsedPlaybackTime] = _positionSec.value
        nowPlaying[MPNowPlayingInfoPropertyPlaybackRate] = rate
        MPNowPlayingInfoCenter.defaultCenter().nowPlayingInfo = nowPlaying.toMap()
    }

    /** Fetches the cover on a background queue and attaches it when it lands. */
    private fun loadArtwork(url: String) {
        val nsUrl = NSURL.URLWithString(url) ?: return
        val task = NSURLSession.sharedSession.dataTaskWithURL(nsUrl) { data: NSData?, _, _ ->
            if (data == null || url != currentArtworkUrl) return@dataTaskWithURL
            val image = UIImage.imageWithData(data) ?: return@dataTaskWithURL
            NSOperationQueue.mainQueue.addOperationWithBlock {
                if (url != currentArtworkUrl) return@addOperationWithBlock
                val size = image.size.useContents { CGSizeMake(width, height) }
                nowPlaying[MPMediaItemPropertyArtwork] = MPMediaItemArtwork(boundsSize = size) { _ -> image }
                publishNowPlaying(rate = if (_isPlaying.value) 1.0 else 0.0)
            }
        }
        task.resume()
    }

    private fun wireRemoteCommands() {
        val center = MPRemoteCommandCenter.sharedCommandCenter()
        center.playCommand.addTargetWithHandler { _ ->
            play(); MPRemoteCommandHandlerStatusSuccess
        }
        center.pauseCommand.addTargetWithHandler { _ ->
            pause(); MPRemoteCommandHandlerStatusSuccess
        }
        center.togglePlayPauseCommand.addTargetWithHandler { _ ->
            if (_isPlaying.value) pause() else play()
            MPRemoteCommandHandlerStatusSuccess
        }
        center.nextTrackCommand.addTargetWithHandler { _ ->
            val handler = onNext
            if (handler != null) { handler(); MPRemoteCommandHandlerStatusSuccess } else MPRemoteCommandHandlerStatusCommandFailed
        }
        center.previousTrackCommand.addTargetWithHandler { _ ->
            val handler = onPrevious
            if (handler != null) { handler(); MPRemoteCommandHandlerStatusSuccess } else MPRemoteCommandHandlerStatusCommandFailed
        }
        center.changePlaybackPositionCommand.addTargetWithHandler { event ->
            val position = (event as? MPChangePlaybackPositionCommandEvent)?.positionTime
            if (position != null) { seekTo(position); MPRemoteCommandHandlerStatusSuccess } else MPRemoteCommandHandlerStatusCommandFailed
        }
    }

    // ---- home-screen widget --------------------------------------------------
    //
    // The WidgetKit extension (iosApp/MusicWidget) reads this from the shared
    // app group; see its README for the one-time Xcode setup.

    private fun publishWidgetState(info: NowPlayingInfo) {
        val shared = NSUserDefaults(suiteName = WIDGET_APP_GROUP)
        shared.setObject(info.title, forKey = "title")
        shared.setObject(info.artist, forKey = "artist")
        shared.setBool(true, forKey = "hasItem")
    }

    private fun clearWidgetState() {
        val shared = NSUserDefaults(suiteName = WIDGET_APP_GROUP)
        shared.setBool(false, forKey = "hasItem")
    }

    private companion object {
        const val WIDGET_APP_GROUP = "group.com.music.app"
    }
}
