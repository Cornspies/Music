package com.music.app.offline

expect object NetworkMonitor {
    fun isMetered(): Boolean
}
