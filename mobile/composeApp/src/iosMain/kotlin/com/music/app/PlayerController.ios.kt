package com.music.app.player

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.useContents
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
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
import platform.AVFoundation.AVPlayerItemStatusReadyToPlay
import platform.AVFoundation.AVPlayerRateDidChangeNotification
import platform.AVFoundation.currentItem
import platform.AVFoundation.currentTime
import platform.AVFoundation.pause
import platform.AVFoundation.play
import platform.AVFoundation.rate
import platform.AVFoundation.replaceCurrentItemWithPlayerItem
import platform.AVFoundation.seekToTime
import platform.CoreGraphics.CGSizeMake
import platform.CoreMedia.CMTimeGetSeconds
import platform.CoreMedia.CMTimeMakeWithSeconds
import platform.Foundation.NSData
import platform.Foundation.NSNotificationCenter
import platform.Foundation.NSNumber
import platform.Foundation.NSOperationQueue
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

    /** One player for the app's lifetime; each track replaces its item. */
    private val player = AVPlayer()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    private var nowPlaying: MutableMap<Any?, Any?> = mutableMapOf()
    private var currentArtworkUrl: String? = null

    // The player stops at the end of an item as if paused; wantsToPlay tells them apart.
    private var reachedEnd = false

    private val endObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVPlayerItemDidPlayToEndTimeNotification, `object` = null, queue = NSOperationQueue.mainQueue) { _ ->
            reachedEnd = true
            _isPlaying.value = false
            onEnded?.invoke()
        }

    // Play/pause arrives as an event rather than by polling the player.
    private val rateObserver: NSObjectProtocol = NSNotificationCenter.defaultCenter
        .addObserverForName(AVPlayerRateDidChangeNotification, `object` = player, queue = NSOperationQueue.mainQueue) { _ ->
            val playing = player.rate > 0f
            if (playing != _isPlaying.value) {
                _isPlaying.value = playing
                publishNowPlaying(rate = if (playing) 1.0 else 0.0)
            }
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

    init {
        configureAudioSession()
        wireRemoteCommands()
        scope.tickPosition(_positionSec, _isPlaying, ::readPosition)
    }

    // Until the item is ready its time isn't meaningful, and a seek made before
    // then (restoring a saved position) is only in _positionSec.
    private fun readPosition(): Double? =
        player.currentItem?.takeIf { it.status == AVPlayerItemStatusReadyToPlay }
            ?.let { CMTimeGetSeconds(player.currentTime()).takeUnless { it.isNaN() } }

    actual fun currentPositionSec(): Double = readPosition() ?: _positionSec.value

    // The rate stays up while buffering and drops to 0 when anything pauses.
    actual fun wantsToPlay(): Boolean = player.rate > 0f || reachedEnd

    actual fun load(url: String, autoplay: Boolean, info: NowPlayingInfo?) {
        _positionSec.value = 0.0
        val nsUrl = if (url.startsWith("/")) NSURL.fileURLWithPath(url) else NSURL.URLWithString(url)
        if (nsUrl == null) return
        reachedEnd = false
        player.replaceCurrentItemWithPlayerItem(AVPlayerItem(uRL = nsUrl))
        if (info != null) setNowPlaying(info)
        if (autoplay) player.play()
    }

    actual fun play() {
        reachedEnd = false
        player.play()
    }

    actual fun pause() {
        player.pause()
    }

    actual fun seekTo(sec: Double) {
        reachedEnd = false
        player.seekToTime(CMTimeMakeWithSeconds(sec, 600))
        _positionSec.value = sec
        publishNowPlaying(rate = if (_isPlaying.value) 1.0 else 0.0, elapsed = sec)
    }

    actual fun release() {
        scope.cancel()
        NSNotificationCenter.defaultCenter.removeObserver(endObserver)
        NSNotificationCenter.defaultCenter.removeObserver(rateObserver)
        NSNotificationCenter.defaultCenter.removeObserver(interruptionObserver)
        player.pause()
        player.replaceCurrentItemWithPlayerItem(null)
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

    private fun publishNowPlaying(rate: Double, elapsed: Double = currentPositionSec()) {
        if (nowPlaying.isEmpty()) return
        nowPlaying[MPNowPlayingInfoPropertyElapsedPlaybackTime] = elapsed
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
        const val WIDGET_APP_GROUP = "group.com.cornspies.music"
    }
}
