package com.music.app.ui.components

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier

/**
 * Wraps a screen so pulling down re-fetches it.
 *
 * The library doesn't push changes, so a song added from the website only shows
 * up here when something asks the server again. This is that gesture.
 *
 * [isRefreshing] should come from the screen's existing loading flag. Those
 * flags gate their full-screen spinner on the data still being null, so during
 * a pull the content stays put and only the pull indicator moves — which is the
 * behaviour people expect from the gesture.
 *
 * The content must be scrollable (a LazyColumn in every current caller) or the
 * gesture has nothing to attach to.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RefreshableContent(
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    // The loading flag also flips for the fetch every screen does when it
    // appears, and that must not look like a pull: the indicator is shown only
    // for a refresh this gesture started, and hidden again once that fetch
    // has come and gone.
    var pulled by remember { mutableStateOf(false) }
    var sawRefreshing by remember { mutableStateOf(false) }
    LaunchedEffect(isRefreshing) {
        if (isRefreshing) {
            sawRefreshing = true
        } else if (sawRefreshing) {
            sawRefreshing = false
            pulled = false
        }
    }

    PullToRefreshBox(
        isRefreshing = pulled && isRefreshing,
        onRefresh = {
            pulled = true
            onRefresh()
        },
        modifier = modifier.fillMaxSize(),
    ) {
        content()
    }
}
