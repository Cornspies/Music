package com.music.app.offline

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import com.music.app.appContext

actual object NetworkMonitor {
    actual fun isMetered(): Boolean {
        val cm = appContext.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return false
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return false
        return !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
    }
}
