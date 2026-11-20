package com.music.app

import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.snapshots.Snapshot
import com.music.app.api.Playlist
import com.music.app.api.Track
import com.music.app.state.AppModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import platform.CarPlay.CPInterfaceController
import platform.CarPlay.CPListItem
import platform.CarPlay.CPListItemAccessoryTypeDisclosureIndicator
import platform.CarPlay.CPListSection
import platform.CarPlay.CPListTemplate
import platform.CarPlay.CPNowPlayingTemplate
import platform.CarPlay.CPTabBarTemplate
import platform.CarPlay.CPTemplate
import platform.UIKit.UIImage

// The app on a car's screen. CarPlay can't show the phone's screens: an audio
// app gets lists and a now-playing screen that the car draws itself. The Swift
// scene delegate (iosApp/iosApp/CarPlaySceneDelegate.swift) hands the car's
// interface over here when it connects.
object CarScreens {
    private var session: CarSession? = null

    fun connect(controller: CPInterfaceController) {
        session?.close()
        session = CarSession(appModel(), controller)
    }

    fun disconnect() {
        session?.close()
        session = null
    }
}

private class CarSession(private val model: AppModel, private val controller: CPInterfaceController) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    // The phone's screens are what normally tells readers of the app's state
    // that it changed. The car can be connected without them.
    private val changed = Channel<Unit>(Channel.CONFLATED)
    private val writes = Snapshot.registerGlobalWriteObserver { changed.trySend(Unit) }

    private val library = list("Library", "music.note", "No songs yet")
    private val playlists = list("Playlists", "music.note.list", "No playlists yet")
    private val favorites = list("Favorites", "heart", "No favorites yet")
    private val downloads = list("Downloads", "arrow.down.circle", "Nothing downloaded")
    private val tabs = CPTabBarTemplate(
        templates = listOf(library, playlists, favorites, downloads).take(CPTabBarTemplate.maximumTabCount.toInt()),
    )

    init {
        scope.launch { for (change in changed) Snapshot.sendApplyNotifications() }
        scope.launch {
            snapshotFlow { model.bootstrapping to (model.user != null) }.collect { (starting, signedIn) ->
                val root = when {
                    signedIn -> tabs
                    starting -> list("Music", null, "Loading…")
                    else -> list("Music", null, "Not signed in", "Open Music on your iPhone to sign in.")
                }
                controller.setRootTemplate(root, animated = false, completion = null)
            }
        }
        scope.launch {
            snapshotFlow { model.tracks.orEmpty() to model.currentTrack?.id }.collect { (tracks, _) ->
                library.updateSections(songs(tracks, context = null))
            }
        }
        scope.launch {
            snapshotFlow { model.favorites.orEmpty() to model.currentTrack?.id }.collect { (tracks, _) ->
                favorites.updateSections(songs(tracks, context = "favorites"))
            }
        }
        scope.launch {
            snapshotFlow { model.downloads.tracks to model.currentTrack?.id }.collect { (tracks, _) ->
                downloads.updateSections(songs(tracks, context = null))
            }
        }
        scope.launch {
            snapshotFlow { model.playlists ?: model.downloads.offlinePlaylists() }.collect { lists ->
                playlists.updateSections(rows(lists.map(::row)))
            }
        }
    }

    fun close() {
        writes.dispose()
        scope.cancel()
    }

    private fun list(title: String, symbol: String?, empty: String, hint: String? = null) =
        CPListTemplate(title = title, sections = emptyList<CPListSection>()).apply {
            tabTitle = title
            tabImage = symbol?.let { UIImage.systemImageNamed(it) }
            emptyViewTitleVariants = listOf(empty)
            emptyViewSubtitleVariants = listOfNotNull(hint)
        }

    // A car shows a limited number of rows, fewer while driving.
    private fun rows(items: List<CPListItem>): List<CPListSection> {
        val shown = items.take(CPListTemplate.maximumItemCount.toInt())
        return if (shown.isEmpty()) emptyList() else listOf(CPListSection(items = shown))
    }

    // Playing a song queues the whole list it is in, as on the phone, however
    // many of its rows the car shows.
    private fun songs(tracks: List<Track>, context: String?): List<CPListSection> {
        if (tracks.isEmpty()) return emptyList()
        val current = model.currentTrack?.id
        val shuffle = CPListItem(text = "Shuffle", detailText = songCount(tracks.size)).apply {
            setImage(UIImage.systemImageNamed("shuffle"))
            handler = { _, done ->
                model.playAll(tracks, playContext = context, shuffled = true)
                showNowPlaying()
                done?.invoke()
            }
        }
        val items = tracks.asSequence().take(CPListTemplate.maximumItemCount.toInt()).map { track ->
            CPListItem(text = track.title, detailText = track.artist.ifBlank { "Unknown artist" }).apply {
                setPlaying(track.id == current)
                handler = { _, done ->
                    model.playTrack(track, tracks, context)
                    showNowPlaying()
                    done?.invoke()
                }
            }
        }
        return rows(listOf(shuffle) + items)
    }

    private fun row(playlist: Playlist) =
        CPListItem(text = playlist.name, detailText = songCount(playlist.trackCount)).apply {
            accessoryType = CPListItemAccessoryTypeDisclosureIndicator
            handler = { _, done ->
                open(playlist)
                done?.invoke()
            }
        }

    private fun open(playlist: Playlist) {
        val template = list(playlist.name, null, "Loading…")
        controller.pushTemplate(template, animated = true, completion = null)
        scope.launch {
            val tracks = try {
                model.api.playlist(playlist.id).tracks
            } catch (_: Exception) {
                model.downloads.offlinePlaylistDetail(playlist.id)?.tracks
            }
            template.emptyViewTitleVariants = listOf(if (tracks == null) "Couldn't load this playlist" else "No songs yet")
            template.updateSections(songs(tracks.orEmpty(), context = "playlist:${playlist.id}"))
        }
    }

    private fun showNowPlaying() {
        val playing: CPTemplate = CPNowPlayingTemplate.sharedTemplate
        if (controller.templates.contains(playing)) {
            controller.popToTemplate(playing, animated = true, completion = null)
        } else {
            controller.pushTemplate(playing, animated = true, completion = null)
        }
    }

    private fun songCount(count: Int) = if (count == 1) "1 song" else "$count songs"
}
