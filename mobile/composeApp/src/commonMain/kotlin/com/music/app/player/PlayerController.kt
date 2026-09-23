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

    val positionSec: StateFlow<Double>

    fun release()
}
