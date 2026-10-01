package com.music.app.player

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

internal fun CoroutineScope.tickPosition(
    position: MutableStateFlow<Double>,
    isPlaying: StateFlow<Boolean>,
    read: () -> Double?,
) = launch {
    combine(position.subscriptionCount, isPlaying) { watchers, playing -> watchers > 0 && playing }
        .distinctUntilChanged()
        .collectLatest { active ->
            read()?.let { position.value = it }
            while (active) {
                delay(250)
                read()?.let { position.value = it }
            }
        }
}
