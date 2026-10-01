package com.music.app.offline

expect class OfflineStore() {

    suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String

    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    fun bytesUsed(): Long

    fun saveMeta(key: String, value: String?)

    fun loadMeta(key: String): String?
}
