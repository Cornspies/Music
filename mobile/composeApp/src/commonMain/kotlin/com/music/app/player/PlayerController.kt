package com.music.app.player

import kotlinx.coroutines.flow.StateFlow

/**
 * Minimal expect/actual audio player.
 *  - Android: Media3 ExoPlayer (see androidMain/PlayerController.android.kt).
 *  - iOS:     AVFoundation AVPlayer (see iosMain/PlayerController.ios.kt).
 *
 * [isPlaying] and [positionSec] are hot state flows that the UI collects with
 * `collectAsState()`. `positionSec` is polled (250–500 ms) — plenty for a seek bar.
 */
expect class PlayerController() {

    /** Invoked on the platform main thread when the current item plays to its end. */
    var onEnded: (() -> Unit)?

    /** Loads a new media URL, replacing any current item. Resets position to 0. */
    fun load(url: String, autoplay: Boolean)

    fun play()

    fun pause()

    fun seekTo(sec: Double)

    val isPlaying: StateFlow<Boolean>

    /** Current playback position in seconds. */
    val positionSec: StateFlow<Double>

    fun release()
}
