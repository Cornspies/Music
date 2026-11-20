package com.music.app.offline

expect class OfflineStore() {

    suspend fun save(trackId: String, extension: String, write: suspend (path: String) -> Unit): String

    fun pathFor(trackId: String): String?

    fun delete(trackId: String)

    fun deleteAll()

    fun bytesUsed(): Long

    // Sizes the files that were there when the app started, off the calling
    // thread. Until it has run, bytesUsed() only counts what was saved since.
    suspend fun measure()

    fun saveExtra(trackId: String, kind: String, bytes: ByteArray)

    fun extraPath(trackId: String, kind: String): String?

    fun readExtra(trackId: String, kind: String): ByteArray?

    fun deleteExtras(trackId: String)

    fun saveMeta(key: String, value: String?)

    // Like saveMeta, but returns only once the values are on disk, so it
    // blocks: not for the main thread.
    fun commitMeta(values: Map<String, String?>)

    fun loadMeta(key: String): String?
}

internal const val PART_SUFFIX = ".part"

// The files in one of the store's folders, each named "<track id>.<rest>".
// Filled from a single listing and updated with every change after that, so
// finding a track's files or adding up the space never reads the folder again.
internal class FileCatalog(names: Iterable<String>) {

    // Track id, then file name, to the file's size: null until measured()
    // has supplied it.
    private val files = HashMap<String, HashMap<String, Long?>>()

    var bytes = 0L
        private set

    init {
        for (name in names) filesOf(name)[name] = if (name.endsWith(PART_SUFFIX)) 0L else null
    }

    fun names(trackId: String): List<String> = files[trackId]?.keys?.toList().orEmpty()

    operator fun contains(name: String): Boolean = files[trackOf(name)]?.containsKey(name) == true

    fun put(name: String, size: Long) {
        remove(name)
        filesOf(name)[name] = size
        bytes += size
    }

    fun remove(name: String) {
        val track = trackOf(name)
        val ofTrack = files[track] ?: return
        bytes -= ofTrack.remove(name) ?: 0L
        if (ofTrack.isEmpty()) files.remove(track)
    }

    fun clear() {
        files.clear()
        bytes = 0L
    }

    fun unmeasured(): List<String> = files.values.flatMap { ofTrack -> ofTrack.filterValues { it == null }.keys }

    // Files that were removed or written again while they were being sized
    // keep what the catalog knows about them.
    fun measured(sizes: Map<String, Long>) {
        for ((name, size) in sizes) {
            val ofTrack = files[trackOf(name)] ?: continue
            if (name in ofTrack && ofTrack[name] == null) {
                ofTrack[name] = size
                bytes += size
            }
        }
    }

    private fun filesOf(name: String) = files.getOrPut(trackOf(name)) { HashMap() }

    private fun trackOf(name: String) = name.substringBefore('.')
}
