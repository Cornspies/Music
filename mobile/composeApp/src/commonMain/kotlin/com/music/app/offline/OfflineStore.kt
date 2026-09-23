package com.music.app.offline

expect class OfflineStore() {

    fun save(trackId: String, extension: String, bytes: ByteArray): String

    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    fun bytesUsed(): Long

    fun saveMeta(key: String, value: String?)

    fun loadMeta(key: String): String?
}
