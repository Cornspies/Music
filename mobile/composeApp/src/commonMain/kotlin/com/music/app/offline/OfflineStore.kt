package com.music.app.offline

expect class OfflineStore() {

    /**
     * Saves a download: [write] fills a temporary file at the path it's given,
     * which then replaces any earlier copy of the track. Returns the final path.
     */
    suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String

    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    fun bytesUsed(): Long

    fun saveMeta(key: String, value: String?)

    fun loadMeta(key: String): String?
}
