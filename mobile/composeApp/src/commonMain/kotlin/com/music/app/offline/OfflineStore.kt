package com.music.app.offline

expect class OfflineStore() {

    suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String

    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    fun bytesUsed(): Long

    fun saveExtra(trackId: String, kind: String, bytes: ByteArray)

    fun extraPath(trackId: String, kind: String): String?

    fun readExtra(trackId: String, kind: String): ByteArray?

    fun deleteExtras(trackId: String)

    fun saveMeta(key: String, value: String?)

    fun loadMeta(key: String): String?
}
