package com.music.app.ui.components

import androidx.compose.runtime.Composable

/**
 * System back handling. Android: real back gesture/button. iOS: no-op
 * (screens always render their own back chevron).
 */
@Composable
expect fun BackHandler(enabled: Boolean, onBack: () -> Unit)
