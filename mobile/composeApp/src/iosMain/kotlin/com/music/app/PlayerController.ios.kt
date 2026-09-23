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

    private var nowPlaying: MutableMap<Any?, Any?> = mutableMapOf()
    private var currentArtworkUrl: String? = null

    private val endObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVPlayerItemDidPlayToEndTimeNotification, `object` = null, queue = null) { _ ->
            _isPlaying.value = false
            onEnded?.invoke()
        }

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
                        configureAudioSession()
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
