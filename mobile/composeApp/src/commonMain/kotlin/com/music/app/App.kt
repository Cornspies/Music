package com.music.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import coil3.ImageLoader
import coil3.compose.setSingletonImageLoaderFactory
import coil3.network.ktor3.KtorNetworkFetcherFactory
import com.music.app.state.AppModel
import com.music.app.ui.components.BackHandler
import com.music.app.ui.components.NowPlayingBar
import com.music.app.ui.components.OfflineBanner
import com.music.app.ui.screens.DownloadsScreen
import com.music.app.ui.screens.FavoritesScreen
import com.music.app.ui.screens.HistoryScreen
import com.music.app.ui.screens.HomeScreen
import com.music.app.ui.screens.LibraryScreen
import com.music.app.ui.screens.LoginScreen
import com.music.app.ui.screens.NowPlayingScreen
import com.music.app.ui.screens.PlaylistDetailScreen
import com.music.app.ui.screens.PlaylistsScreen
import com.music.app.ui.screens.ProfileScreen
import com.music.app.ui.screens.SearchScreen
import com.music.app.ui.screens.UploadScreen
import com.music.app.ui.theme.MusicTheme

/**
 * Navigation destinations. Bottom-bar tabs are the root screens; everything else
 * (PlaylistDetail, Upload, NowPlaying) is pushed on top of the back stack.
 */
sealed interface Screen {
    data object Home : Screen
    data object Library : Screen
    data object Search : Screen
    data object Playlists : Screen
    data object Favorites : Screen
    data object Upload : Screen
    data object Downloads : Screen
    data object Profile : Screen
    data object History : Screen
    data object NowPlaying : Screen
    data class PlaylistDetail(val id: String) : Screen
}

/** The app-wide state holder, provided at the root and consumed by all screens. */
val LocalAppModel = staticCompositionLocalOf<AppModel> { error("AppModel is not provided") }

@Composable
fun App() {
    val model = remember { AppModel() }
    DisposableEffect(model) {
        onDispose { model.destroy() }
    }

    // Coil image loader with the Ktor network engine (shared by all cover art).
    setSingletonImageLoaderFactory { context ->
        ImageLoader.Builder(context)
            .components { add(KtorNetworkFetcherFactory()) }
            .build()
    }

    // A share link usually arrives before sign-in has finished, so it waits
    // until there's a session and is then opened exactly once.
    val pendingLink = DeepLinks.pendingPlaylistId
    LaunchedEffect(pendingLink, model.user) {
        if (pendingLink != null && model.user != null) {
            DeepLinks.consume()?.let { model.openSharedPlaylist(it) }
        }
    }

    CompositionLocalProvider(LocalAppModel provides model) {
        MusicTheme {
            Root()
        }
    }
}

@Composable
private fun Root() {
    val model = LocalAppModel.current
    if (model.bootstrapping) {
        Box(Modifier.fillMaxSize(), contentAlignment = androidx.compose.ui.Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }
    if (model.user == null) {
        LoginScreen()
    } else {
        MainScreen()
    }
}

@Composable
private fun MainScreen() {
    val model = LocalAppModel.current
    val current = model.backStack.last()

    BackHandler(enabled = model.backStack.size > 1) { model.pop() }

    // The full-screen player is an overlay, not a destination of its own:
    // the screen it was opened from stays composed underneath (bars and all),
    // so swiping the player down reveals it rather than a black void.
    val playerOpen = current == Screen.NowPlaying
    val shown = if (playerOpen) {
        model.backStack.getOrNull(model.backStack.size - 2) ?: Screen.Home
    } else {
        current
    }

    Box(Modifier.fillMaxSize()) {
        Scaffold(
            containerColor = MaterialTheme.colorScheme.background,
            bottomBar = {
                Column {
                    if (model.currentTrack != null) {
                        NowPlayingBar()
                    }
                    // Tabs stay put on pushed screens like a playlist: they are
                    // normal browsing destinations, and selectTab resets the
                    // stack so "back" still never crosses tabs.
                    BottomTabs(selected = shown)
                }
            },
        ) { padding ->
            Column(
                Modifier
                    .fillMaxSize()
                    .padding(padding),
            ) {
                // Mounted once for the whole app: every screen keeps its normal
                // layout offline and just gains this strip at the top.
                OfflineBanner()
                Box(Modifier.fillMaxSize()) {
                    when (shown) {
                        Screen.Home -> HomeScreen()
                        Screen.Library -> LibraryScreen()
                        Screen.Search -> SearchScreen()
                        Screen.Playlists -> PlaylistsScreen()
                        Screen.Favorites -> FavoritesScreen()
                        Screen.Upload -> UploadScreen()
                        Screen.Downloads -> DownloadsScreen()
                        Screen.Profile -> ProfileScreen()
                        Screen.History -> HistoryScreen()
                        Screen.NowPlaying -> HomeScreen() // never: the player is always the overlay
                        is Screen.PlaylistDetail -> PlaylistDetailScreen(shown)
                    }
                }
            }
        }

        if (playerOpen) {
            // Outside the Scaffold, so the on-background content colour that
            // Text and Icon default to has to be provided by hand.
            CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onBackground) {
                NowPlayingScreen()
            }
        }
    }
}

@Composable
private fun BottomTabs(selected: Screen) {
    val model = LocalAppModel.current
    val current = selected
    NavigationBar(containerColor = MaterialTheme.colorScheme.surface) {
        NavigationBarItem(
            selected = current == Screen.Home,
            onClick = { model.selectTab(Screen.Home) },
            icon = { Icon(Icons.Filled.Home, contentDescription = null) },
            label = { Text("Home") },
        )
        NavigationBarItem(
            selected = current == Screen.Library,
            onClick = { model.selectTab(Screen.Library) },
            icon = { Icon(Icons.Filled.LibraryMusic, contentDescription = null) },
            label = { Text("Library") },
        )
        NavigationBarItem(
            selected = current == Screen.Search,
            onClick = { model.selectTab(Screen.Search) },
            icon = { Icon(Icons.Filled.Search, contentDescription = null) },
            label = { Text("Search") },
        )
        NavigationBarItem(
            selected = current == Screen.Playlists,
            onClick = { model.selectTab(Screen.Playlists) },
            icon = { Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = null) },
            label = { Text("Playlists") },
        )
        NavigationBarItem(
            selected = current == Screen.Favorites,
            onClick = { model.selectTab(Screen.Favorites) },
            icon = {
                Icon(
                    if (current == Screen.Favorites) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                    contentDescription = null,
                )
            },
            label = { Text("Favorites") },
        )
    }
}
