package com.music.app.state

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.music.app.api.ApiClient
import com.music.app.api.ApiException
import com.music.app.api.ListenAddRequest
import com.music.app.api.ListenControlRequest
import com.music.app.api.ListenCreateRequest
import com.music.app.api.ListenEntry
import com.music.app.api.ListenInvite
import com.music.app.api.ListenSettingsRequest
import com.music.app.api.ListenState
import com.music.app.api.Track
import com.music.app.api.friendlyError
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.DurationUnit
import kotlin.time.TimeMark
import kotlin.time.TimeSource

const val MAX_LISTEN_GUESTS = 20
const val DEFAULT_LISTEN_GUESTS = 5

/** Songs queued after the current one, at most. */
const val MAX_LISTEN_QUEUE = 1000

// How often the player is checked against the session while it plays.
private const val SYNC_INTERVAL_MS = 3_000L

/** Why the player is asked to match the session. */
enum class Follow {
    /** The session changed. */
    CHANGE,

    /** A session was just started or joined here: keep what already plays. */
    ADOPT,

    /** Played or seeked here: catch up at once. */
    JUMP,

    /** The regular check while playing: only correct drift. */
    TICK,
}

/**
 * Listen together. The server keeps a session's timeline; this device follows
 * it through a long poll and plays the audio itself (AppModel.followSession).
 * Whoever may control the session (the host, or everyone if the host allows
 * it) changes what plays with commands. Nobody pauses for everyone: pausing
 * stops this device only, and playing again jumps to where everyone else is.
 */
class ListenTogether(
    private val api: ApiClient,
    private val scope: CoroutineScope,
    private val myId: () -> String?,
    /** Makes the player match the session. */
    private val follow: (Follow) -> Unit,
    /** This device stopped following; [message] says why, when there's something to say. */
    private val left: (message: String?) -> Unit,
    private val notify: (String) -> Unit,
) {
    var session by mutableStateOf<ListenState?>(null)
        private set
    var entries by mutableStateOf<List<ListenEntry>>(emptyList())
        private set

    /** Paused on this device while the session plays on. */
    var localPaused by mutableStateOf(false)
        private set

    /** The entries' tracks: the queue the player shows. */
    var queueTracks: List<Track> = emptyList()
        private set

    val active: Boolean get() = session != null

    private var receivedAt: TimeMark = TimeSource.Monotonic.markNow()
    private var pollJob: Job? = null
    private var syncJob: Job? = null

    // States at or below this version predate a change made on this device
    // and would undo it for a moment.
    private var optimisticVersion = -1L
    private var knownMembers: Set<String>? = null

    // Set while this device leaves, so the poll noticing doesn't announce it too.
    private var leaving = false

    fun currentEntry(): ListenEntry? = session?.let { st -> entries.firstOrNull { it.id == st.current } }

    fun upcoming(): List<ListenEntry> {
        val index = entries.indexOfFirst { it.id == session?.current }
        return if (index < 0) entries else entries.drop(index + 1)
    }

    /** Where the session is now. */
    fun expectedPosition(): Double {
        val st = session ?: return 0.0
        val pos = if (st.playing) st.position + receivedAt.elapsedNow().toDouble(DurationUnit.SECONDS) else st.position
        val duration = currentEntry()?.track?.duration ?: 0.0
        return if (duration > 0) minOf(pos, duration) else pos
    }

    fun canRemove(entry: ListenEntry): Boolean = session?.canControl == true || entry.addedBy.id == myId()

    private fun apply(state: ListenState, adopt: Boolean = false) {
        val current = session
        val same = current?.id == state.id
        if (current != null && same && (state.version < current.version || state.version <= optimisticVersion)) return
        optimisticVersion = -1
        val fresh = state.entries
        if (fresh != null) {
            entries = fresh
            queueTracks = fresh.map { it.track }
        } else if (!same) {
            entries = emptyList()
            queueTracks = emptyList()
        }
        receivedAt = TimeSource.Monotonic.markNow()
        announceMembers(state)
        session = state.copy(entries = null)
        follow(if (adopt) Follow.ADOPT else Follow.CHANGE)
    }

    private fun announceMembers(state: ListenState) {
        val ids = state.members.map { it.id }.toSet()
        val before = knownMembers
        knownMembers = ids
        if (before == null) return
        val me = myId()
        state.members.filter { it.id !in before && it.id != me }.forEach { notify("${it.username} joined the session") }
        val gone = before.count { it !in ids && it != me }
        if (gone == 1) notify("Someone left the session") else if (gone > 1) notify("$gone people left the session")
    }

    private fun enter(state: ListenState, followNow: Boolean) {
        stopJobs()
        knownMembers = null
        optimisticVersion = -1
        session = null
        entries = emptyList()
        queueTracks = emptyList()
        localPaused = !followNow
        apply(state, adopt = true)
        val id = state.id
        pollJob = scope.launch {
            var backoff = 1_000L
            while (true) {
                val st = session ?: break
                if (st.id != id) break
                try {
                    apply(api.listenPoll(id, st.version, st.queueVersion))
                    backoff = 1_000L
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    if (gone(e)) break
                    delay(backoff.milliseconds)
                    backoff = minOf(backoff * 2, 15_000L)
                }
            }
        }
        syncJob = scope.launch {
            while (true) {
                delay(SYNC_INTERVAL_MS.milliseconds)
                if (session?.playing == true && !localPaused) follow(Follow.TICK)
            }
        }
    }

    private fun stopJobs() {
        pollJob?.cancel()
        pollJob = null
        syncJob?.cancel()
        syncJob = null
    }

    private fun exit(message: String?) {
        stopJobs()
        val was = session != null
        session = null
        entries = emptyList()
        queueTracks = emptyList()
        localPaused = false
        knownMembers = null
        if (was) left(message) else message?.let(notify)
    }

    /** Handles the server saying this device isn't in the session any more. */
    private fun gone(e: Exception): Boolean {
        val error = e as? ApiException ?: return false
        if (error.status == 401) {
            exit(null)
            return true
        }
        val reason = error.reason?.takeIf { error.status == 404 } ?: return false
        if (leaving) {
            exit(null)
            return true
        }
        val st = session
        exit(
            when {
                reason == "removed" -> "The host removed you from the listening session"
                reason == "left" -> "You left the listening session"
                reason == "timeout" && st?.isHost == true -> "Your listening session ended while you were away"
                reason == "timeout" -> "The listening session ended: ${st?.host?.username ?: "the host"} was away too long"
                st != null && !st.isHost -> "${st.host.username} ended the listening session"
                else -> "The listening session ended"
            },
        )
        return true
    }

    /** Sends a change and applies the session it answers with. */
    private suspend fun send(quiet: Boolean = false, request: suspend (id: String, queue: Long) -> ListenState): Boolean {
        val st = session ?: return false
        return try {
            apply(request(st.id, st.queueVersion))
            true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            optimisticVersion = -1
            if (!gone(e)) {
                if (!quiet) notify(sentence(friendlyError(e)))
                refresh()
            }
            false
        }
    }

    private fun launchSend(quiet: Boolean = false, request: suspend (id: String, queue: Long) -> ListenState) {
        scope.launch { send(quiet, request) }
    }

    private suspend fun refresh() {
        val st = session ?: return
        try {
            apply(api.listenPoll(st.id, 0, st.queueVersion))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            gone(e)
        }
    }

    /** Back in the foreground: the poll may have been asleep. */
    fun catchUp() {
        scope.launch { refresh() }
    }

    private fun denied(what: String) {
        session?.let { notify("Only ${it.host.username} can $what in this session") }
    }

    /** Plays or pauses this device only; playing catches up with the session. */
    private fun followHere(play: Boolean) {
        localPaused = !play
        follow(if (play) Follow.JUMP else Follow.CHANGE)
    }

    /** Starts a session with [current] (the music starts with it) and what's queued after it. */
    suspend fun start(
        capacity: Int,
        everyoneCanControl: Boolean,
        current: Track?,
        position: Double,
        upcoming: List<String>,
    ) {
        val state = api.listenCreate(
            ListenCreateRequest(
                capacity = capacity,
                everyoneCanControl = everyoneCanControl,
                trackId = current?.id,
                position = position,
                upcoming = if (current != null) upcoming.take(MAX_LISTEN_QUEUE) else emptyList(),
            ),
        )
        enter(state, followNow = true)
    }

    suspend fun invite(code: String): ListenInvite = api.listenInvite(code)

    suspend fun join(code: String) {
        enter(api.listenJoin(code), followNow = true)
    }

    /**
     * Picks up the session this account is in, e.g. after the app restarted.
     * Unless [followNow], it stays paused here until played.
     */
    suspend fun restore(followNow: Boolean = false): Boolean {
        val state = try {
            api.listenCurrent()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        } ?: return false
        enter(state, followNow)
        return true
    }

    /** Leaves, or for the host ends the session for everyone. */
    suspend fun leave() {
        val st = session ?: return
        leaving = true
        try {
            if (st.isHost) api.listenEnd(st.id) else api.listenLeave(st.id)
        } catch (e: ApiException) {
            if (e.status != 404) throw e
        } finally {
            leaving = false
        }
        exit(if (st.isHost) "You ended the listening session" else "You left the listening session")
    }

    /** Signed out: stop following, without telling the server. */
    fun signedOut() = exit(null)

    fun togglePlay(playingHere: Boolean) {
        val st = session ?: return
        if (st.playing) followHere(localPaused || !playingHere) else queueEnded()
    }

    /** The session only stops when its queue runs out. */
    private fun queueEnded() {
        val st = session ?: return
        notify(if (st.canControl) "The queue has ended. Add a song to keep going." else "The queue has ended.")
    }

    /** The player stopped without the app asking: the lock screen, a call, unplugged headphones. */
    fun stoppedHere() {
        if (session?.playing == true && !localPaused) localPaused = true
    }

    /** The player started without the app asking, such as from the lock screen. */
    fun resumedHere() {
        val st = session ?: return
        when {
            // Nothing to follow: pause it again.
            !st.playing -> follow(Follow.CHANGE)
            localPaused -> followHere(true)
        }
    }

    fun next() {
        if (session?.canControl != true) return denied("skip")
        launchSend { id, q -> api.listenControl(id, q, ListenControlRequest("next")) }
    }

    fun previous() {
        if (session?.canControl != true) return denied("skip")
        launchSend { id, q -> api.listenControl(id, q, ListenControlRequest("previous")) }
    }

    fun seek(seconds: Double) {
        val st = session ?: return
        if (!st.canControl) return denied("skip around")
        optimisticVersion = st.version
        receivedAt = TimeSource.Monotonic.markNow()
        session = st.copy(position = seconds)
        follow(Follow.JUMP)
        launchSend { id, q -> api.listenControl(id, q, ListenControlRequest("seek", position = seconds)) }
    }

    fun jump(entry: ListenEntry) {
        if (session?.canControl != true) return denied("choose what plays")
        launchSend { id, q -> api.listenControl(id, q, ListenControlRequest("jump", entryId = entry.id)) }
    }

    /** Queues songs at the "end", "next", or "now" (next, and start them). */
    fun add(tracks: List<Track>, mode: String) {
        if (session?.canControl != true) return denied("choose what plays")
        if (tracks.isEmpty()) return
        val adding = tracks.take((MAX_LISTEN_QUEUE - upcoming().size).coerceAtLeast(0))
        if (adding.isEmpty()) return notify("The queue is full ($MAX_LISTEN_QUEUE songs)")
        scope.launch {
            val ok = send { id, q -> api.listenAdd(id, q, ListenAddRequest(adding.map { it.id }, mode)) }
            if (!ok) return@launch
            val left = tracks.size - adding.size
            when {
                left > 0 -> notify("Added ${adding.size} songs. The queue is full, so the other $left didn't fit.")
                adding.size > 1 -> notify("Added ${adding.size} songs to the queue")
                mode == "next" -> notify("Playing “${adding[0].title}” next")
                mode == "end" -> notify("Added “${adding[0].title}” to the queue")
            }
        }
    }

    fun remove(entry: ListenEntry) {
        if (!canRemove(entry)) return denied("remove other people's songs")
        launchSend { id, q -> api.listenRemove(id, q, entry.id) }
    }

    /** Empties what's queued after the current song, for everyone; only the host may. */
    fun clear() {
        if (session?.isHost != true) return denied("clear the queue")
        launchSend { id, q -> api.listenClear(id, q) }
    }

    fun trackEnded(entryId: String?) {
        if (entryId.isNullOrEmpty()) return
        launchSend(quiet = true) { id, q -> api.listenControl(id, q, ListenControlRequest("ended", entryId = entryId)) }
    }

    fun updateSettings(capacity: Int? = null, everyoneCanControl: Boolean? = null) {
        launchSend { id, q -> api.listenSettings(id, q, ListenSettingsRequest(capacity, everyoneCanControl)) }
    }

    fun removeMember(userId: String) {
        launchSend { id, q -> api.listenRemoveMember(id, q, userId) }
    }
}

private fun sentence(message: String): String =
    message.replaceFirstChar { it.uppercase() }.let { if (it.last() in ".?!") it else "$it." }
