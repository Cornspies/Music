package com.music.app

import com.music.app.state.insecureServerWarning
import kotlin.test.Test
import kotlin.test.assertNotNull
import kotlin.test.assertNull

class ServerWarningTest {

    @Test
    fun localAddressesOverHttpAreFine() {
        for (url in listOf(
            "http://localhost:3001",
            "http://192.168.1.5:3001",
            "http://10.0.2.2:3999/music",
            "http://music.local",
            "http://[fd12:3456::1]:3001",
            "http://[fe80::1]",
            "http://[::1]:3001",
            "https://music.example.org",
        )) {
            assertNull(insecureServerWarning(url), url)
        }
    }

    @Test
    fun publicHostsOverHttpAreWarnedAbout() {
        for (url in listOf(
            "http://music.example.org",
            "http://fdroid.example.org",
            "http://fcmusic.net:8080",
            "http://192.168.1.5:80@evil.example",
            "http://192.168.1.5@evil.example/path",
            "http://8.8.8.8",
        )) {
            assertNotNull(insecureServerWarning(url), url)
        }
    }
}
