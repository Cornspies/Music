package com.music.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.ui.Alignment
import androidx.compose.ui.unit.dp
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.Search
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.LifecycleStartEffect
import coil3.ImageLoader
import coil3.compose.setSingletonImageLoaderFactory
import coil3.network.ktor3.KtorNetworkFetcherFactory
import com.music.app.state.AppModel
import com.music.app.ui.components.BackHandler
import com.music.app.ui.components.NowPlayingBar
import com.music.app.ui.components.OfflineBanner
import com.music.app.ui.screens.DownloadsScreen
import com.music.app.ui.screens.DuplicatesScreen
import com.music.app.ui.screens.FavoritesScreen
import com.music.app.ui.screens.HistoryScreen
import com.music.app.ui.screens.HomeScreen
import com.music.app.ui.screens.LibraryScreen
import com.music.app.ui.screens.ListenInviteDialog
import com.music.app.ui.screens.ListenScreen
import com.music.app.ui.screens.LoginScreen
import com.music.app.ui.screens.NowPlayingScreen
import com.music.app.ui.screens.PlaylistDetailScreen
import com.music.app.ui.screens.PlaylistsScreen
import com.music.app.ui.screens.ProfileScreen
import com.music.app.ui.screens.SearchScreen
import com.music.app.ui.screens.UploadScreen
import com.music.app.ui.theme.MusicTheme

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
    data object Duplicates : Screen
    data object NowPlaying : Screen
    data object Listen : Screen
    data class PlaylistDetail(val id: String) : Screen
}

val LocalAppModel = staticCompositionLocalOf<AppModel> { error("AppModel is not provided") }

@Composable
fun App() {
    val model = remember { AppModel() }
    DisposableEffect(model) {
        onDispose { model.destroy() }
    }

    setSingletonImageLoaderFactory { context ->
        ImageLoader.Builder(context)
            .components { add(KtorNetworkFetcherFactory(httpClient = { model.api.httpClient })) }
            .build()
    }

    val pendingLink = DeepLinks.pendingPlaylistId
    LaunchedEffect(pendingLink, model.user) {
        if (pendingLink != null && model.user != null) {
            DeepLinks.consume()?.let { model.openSharedPlaylist(it) }
        }
    }
    val pendingShare = DeepLinks.pendingShareToken
    LaunchedEffect(pendingShare, model.user) {
        if (pendingShare != null && model.user != null) {
            DeepLinks.consumeShareToken()?.let { model.openSharedTrack(it) }
        }
    }
    // Joining takes an account, so an invite waits until someone is signed in.
    val pendingListen = DeepLinks.pendingListenCode
    LaunchedEffect(pendingListen, model.user) {
        if (pendingListen != null && model.user != null) {
            DeepLinks.consumeListenCode()?.let { model.listenInvite = it }
        }
    }

    LifecycleResumeEffect(model) {
        model.onForeground()
        onPauseOrDispose {}
    }
    LifecycleStartEffect(model) {
        onStopOrDispose { model.onBackground() }
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
    val update = model.availableUpdate
    if (update != null && update.version != model.dismissedUpdateVersion && model.user != null) {
        val uriHandler = LocalUriHandler.current
        AlertDialog(
            onDismissRequest = { model.dismissUpdate() },
            title = { Text("Update available") },
            text = { Text("Music ${update.version} is on your server.") },
            confirmButton = {
                TextButton(onClick = {
                    model.updateDownloadUrl()?.let { uriHandler.openUri(it) }
                    model.dismissUpdate()
                }) { Text("Download") }
            },
            dismissButton = { TextButton(onClick = { model.dismissUpdate() }) { Text("Later") } },
        )
    }
    val invite = model.listenInvite
    if (invite != null && model.user != null) {
        ListenInviteDialog(invite, onDismiss = { model.listenInvite = null })
    }
    model.shareLinkError?.let { message ->
        AlertDialog(
            onDismissRequest = { model.shareLinkError = null },
            title = { Text("Couldn't open the link") },
            text = { Text(message) },
            confirmButton = { TextButton(onClick = { model.shareLinkError = null }) { Text("OK") } },
        )
    }
}

@Composable
private fun MainScreen() {
    val model = LocalAppModel.current
    val current = model.backStack.last()

    BackHandler(enabled = model.backStack.size > 1) { model.pop() }

    val playerOpen = current == Screen.NowPlaying
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(model) {
        model.notices.collect { snackbar.showSnackbar(it) }
    }
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
                    BottomTabs(selected = shown)
                }
            },
        ) { padding ->
            Column(
                Modifier
                    .fillMaxSize()
                    .padding(padding),
            ) {
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
                        Screen.Duplicates -> DuplicatesScreen()
                        Screen.NowPlaying -> HomeScreen()
                        Screen.Listen -> ListenScreen()
                        is Screen.PlaylistDetail -> PlaylistDetailScreen(shown)
                    }
                }
            }
        }

        if (playerOpen) {
            CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onBackground) {
                NowPlayingScreen()
            }
        }

        // Above the player too; there it goes at the top, clear of the controls.
        SnackbarHost(
            snackbar,
            if (playerOpen) {
                Modifier.align(Alignment.TopCenter).statusBarsPadding().padding(top = 48.dp)
            } else {
                Modifier
                    .align(Alignment.BottomCenter)
                    .navigationBarsPadding()
                    .padding(bottom = if (model.currentTrack != null) 136.dp else 80.dp)
            },
        )
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
