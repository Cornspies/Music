package com.music.app.offline

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Network.nw_path_is_expensive
import platform.Network.nw_path_monitor_create
import platform.Network.nw_path_monitor_set_queue
import platform.Network.nw_path_monitor_set_update_handler
import platform.Network.nw_path_monitor_start
import platform.darwin.dispatch_get_main_queue
import kotlin.concurrent.Volatile

/**
 * NWPathMonitor keeps [expensive] current; iOS marks cellular and personal
 * hotspots as expensive, which is what "metered" means here.
 */
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
        // Touching the property makes sure the lazy monitor has started.
        monitor.hashCode()
        return expensive
    }
}
