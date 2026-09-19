package com.music.app.player

import kotlinx.coroutines.flow.StateFlow

/**
 * What the OS shows while a track plays: the lock screen, the notification
 * shade, CarPlay/Android Auto, headphone displays. Both platforms need the
 * same handful of fields.
 */
data class NowPlayingInfo(
    val title: String,
    val artist: String,
    val album: String = "",
    /** Seconds, 0 when unknown. */
    val durationSec: Double = 0.0,
    /** Cover art URL (already carrying its media token), or null. */
    val artworkUrl: String? = null,
)

/**
 * Minimal expect/actual audio player.
 *  - Android: Media3 ExoPlayer inside a MediaSessionService, driven through a
 *    MediaController so playback survives the activity and shows in the
 *    notification shade (see androidMain/PlayerController.android.kt).
 *  - iOS:     AVFoundation AVPlayer with MPNowPlayingInfoCenter and the remote
 *    command centre wired up (see iosMain/PlayerController.ios.kt).
 *
 * [isPlaying] and [positionSec] are hot state flows that the UI collects with
 * `collectAsState()`. `positionSec` is polled (250–500 ms) — plenty for a seek bar.
 *
 * The queue lives in the app, not the player: "next"/"previous" from the
 * lock screen or a headset are forwarded through [onNext]/[onPrevious].
 */
expect class PlayerController() {

    /** Invoked on the platform main thread when the current item plays to its end. */
    var onEnded: (() -> Unit)?

    /** Skip requests from outside the app (notification, lock screen, headset). */
    var onNext: (() -> Unit)?
    var onPrevious: (() -> Unit)?

    /**
     * Loads a new media URL, replacing any current item. Resets position to 0.
     * [info] is what the system shows for it; null keeps the previous text.
     */
    fun load(url: String, autoplay: Boolean, info: NowPlayingInfo? = null)

    fun play()

    fun pause()

    fun seekTo(sec: Double)

    val isPlaying: StateFlow<Boolean>

    /** Current playback position in seconds. */
    val positionSec: StateFlow<Double>

    fun release()
}
