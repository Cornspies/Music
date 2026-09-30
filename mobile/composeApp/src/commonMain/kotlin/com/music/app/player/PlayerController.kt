package com.music.app.player

import kotlinx.coroutines.flow.StateFlow

data class NowPlayingInfo(
    val title: String,
    val artist: String,
    val album: String = "",
    val durationSec: Double = 0.0,
    val artworkUrl: String? = null,
)

expect class PlayerController() {

    var onEnded: (() -> Unit)?

    var onNext: (() -> Unit)?
    var onPrevious: (() -> Unit)?

    fun load(url: String, autoplay: Boolean, info: NowPlayingInfo? = null)

    fun play()

    fun pause()

    fun seekTo(sec: Double)

    val isPlaying: StateFlow<Boolean>

    /**
     * For showing progress: only kept current while playing and something
     * collects it. Collect it with collectAsStateWithLifecycle, so reading
     * stops while the app is in the background.
     */
    val positionSec: StateFlow<Double>

    /** Asks the player for its position right now; use this outside the UI. */
    fun currentPositionSec(): Double

    /**
     * Whether playback is wanted, as opposed to [isPlaying]: still true while
     * buffering and just after a track played to its end, false once anyone
     * paused it (the app, the lock screen, or the system for a call or
     * unplugged headphones).
     */
    fun wantsToPlay(): Boolean

    fun release()
}
