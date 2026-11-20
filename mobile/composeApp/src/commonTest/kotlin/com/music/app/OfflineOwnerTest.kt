package com.music.app

import com.music.app.offline.isForeign
import com.music.app.offline.offlineOwner
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class OfflineOwnerTest {

    @Test
    fun aServerIsTheSameHoweverItWasTyped() {
        val owner = offlineOwner("https://music.example.org", "u1")
        for (typed in listOf(
            "https://music.example.org/",
            "  https://MUSIC.Example.org  ",
            "HTTPS://music.example.org:443",
            "https://music.example.org:443//",
        )) {
            assertEquals(owner, offlineOwner(typed, "u1"), typed)
        }
        assertEquals(offlineOwner("http://music.local", "u1"), offlineOwner("http://music.local:80/", "u1"))
        assertEquals(
            offlineOwner("http://192.168.1.5:3001/music", "u1"),
            offlineOwner("http://192.168.1.5:3001/music/", "u1"),
        )
    }

    @Test
    fun anotherServerOrAccountIsSomeoneElse() {
        val owner = offlineOwner("https://music.example.org", "u1")
        for (other in listOf(
            offlineOwner("https://music.example.org", "u2"),
            offlineOwner("https://other.example.org", "u1"),
            offlineOwner("http://music.example.org", "u1"),
            offlineOwner("https://music.example.org:8443", "u1"),
            offlineOwner("https://music.example.org/second", "u1"),
        )) {
            assertTrue(isForeign(owner, other), other)
        }
    }

    @Test
    fun onlyAnotherOwnersDataIsForeign() {
        val account = offlineOwner("https://music.example.org", "u1")
        assertFalse(isForeign(account, account))
        assertFalse(isForeign(null, account))
        assertTrue(isForeign(offlineOwner("https://music.example.org", "u2"), account))
    }

    // What is stored on devices: a different result here means every existing
    // download is taken for someone else's and cleared at the next sign-in.
    @Test
    fun theStoredFormDoesNotChange() {
        assertEquals("u1@https://music.example.org/music", offlineOwner("https://Music.Example.org:443/music/", "u1"))
        assertEquals("u1@http://192.168.1.5:3001", offlineOwner("http://192.168.1.5:3001", "u1"))
    }
}
