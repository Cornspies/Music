package com.music.app.offline

/**
 * Whether the current connection is one the user would mind big downloads
 * on. "Metered" is the platform's own judgement (mobile data, a hotspot the
 * OS knows is metered); Wi-Fi and Ethernet count as unmetered.
 */
expect object NetworkMonitor {
    /** True when downloads should be held back under a Wi-Fi-only setting. */
    fun isMetered(): Boolean
}
