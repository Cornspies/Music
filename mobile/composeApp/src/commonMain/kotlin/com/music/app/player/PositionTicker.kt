package com.music.app.player

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * Keeps [position] current for the screens that show it: four times a second
 * while playing and something is collecting it, and not at all otherwise, so a
 * phone playing in a pocket isn't woken for a progress bar nobody sees. The UI
 * collects with collectAsStateWithLifecycle, which stops in the background.
 * [read] asks the player for its position, or returns null when it has none.
 */
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
