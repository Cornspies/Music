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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RefreshableContent(
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
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
