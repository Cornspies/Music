package com.music.app.offline

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Network.nw_path_is_expensive
import platform.Network.nw_path_monitor_create
import platform.Network.nw_path_monitor_set_queue
import platform.Network.nw_path_monitor_set_update_handler
import platform.Network.nw_path_monitor_start
import platform.darwin.dispatch_get_main_queue
import kotlin.concurrent.Volatile

@OptIn(ExperimentalForeignApi::class)
actual object NetworkMonitor {
    @Volatile private var expensive = false

    private val monitor = nw_path_monitor_create().also { monitor ->
        nw_path_monitor_set_update_handler(monitor) { path ->
            expensive = path != null && nw_path_is_expensive(path)
        }
        nw_path_monitor_set_queue(monitor, dispatch_get_main_queue())
        nw_path_monitor_start(monitor)
    }

    actual fun isMetered(): Boolean {
        monitor.hashCode()
        return expensive
    }
}
