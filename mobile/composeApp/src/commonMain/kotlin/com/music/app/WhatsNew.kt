package com.music.app

// What changed in each version of the app, shown once after an update. A few
// points a listener would notice, newest version first. A version without an
// entry shows nothing.
internal val WHATS_NEW: Map<String, List<String>> = mapOf(
    "0.7.0" to listOf(
        "Hold a song to play it next or add it to the queue",
        "Playlists have an Add to queue button",
        "Downloads stay when your session runs out, and another account on this phone doesn't see them",
        "The app starts faster with many downloads",
    ),
)

// The points to show for the running version, or null. A first start has no
// earlier version to compare with; an install from before this note existed
// has none either, but is signed in already.
internal fun whatsNew(
    seen: String?,
    current: String?,
    signedIn: Boolean,
    notes: Map<String, List<String>> = WHATS_NEW,
): List<String>? {
    val updated = if (seen != null) seen != current else signedIn
    return notes[current]?.takeIf { updated && it.isNotEmpty() }
}
