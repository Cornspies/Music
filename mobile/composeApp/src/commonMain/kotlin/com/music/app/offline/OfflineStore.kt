package com.music.app.offline

/**
 * Platform-backed storage for downloaded audio plus a small metadata sidecar.
 *
 *  - Android: files under `filesDir/offline`, metadata in the "music-offline" prefs.
 *  - iOS:     files under Application Support/offline, metadata in NSUserDefaults.
 *
 * Audio is stored as `<trackId>.<ext>`; the extension is kept because both
 * players sniff the container format from it. [pathFor] returns a plain
 * filesystem path (not a URL) — PlayerController turns it into a file URL.
 */
expect class OfflineStore() {

    /**
     * Writes [bytes] for [trackId], replacing any previous download, and returns
     * the absolute path. Writes to a temporary file first and renames, so an
     * interrupted download can never leave a half-written file that looks valid.
     */
    fun save(trackId: String, extension: String, bytes: ByteArray): String

    /** Absolute path of the downloaded file, or null when not downloaded. */
    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    /** Total bytes occupied by downloaded audio. */
    fun bytesUsed(): Long

    /** Small key/value sidecar for the downloaded-track index and cached user. */
    fun saveMeta(key: String, value: String?)

    fun loadMeta(key: String): String?
}
