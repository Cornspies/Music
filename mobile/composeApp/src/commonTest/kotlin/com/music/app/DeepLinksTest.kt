package com.music.app

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class DeepLinksTest {

    private fun reset() {
        DeepLinks.rejectForeign("about:blank")
        DeepLinks.consume()
        DeepLinks.consumeShareToken()
        DeepLinks.consumeListenCode()
        DeepLinks.consumeInvite()
    }

    @Test
    fun unwrapsAppSchemeLinks() {
        reset()
        DeepLinks.handle("cornspies-music://open?url=https%3A%2F%2Fmusic.example.org%2Fplaylists%2Fabc-123")
        assertEquals("abc-123", DeepLinks.pendingPlaylistId)
        assertEquals("https://music.example.org", DeepLinks.linkServer)
    }

    @Test
    fun keepsTheServerPath() {
        reset()
        DeepLinks.handle("cornspies-music://open?url=https%3A%2F%2Fexample.org%2Fmusic%2Fs%2Ftok_1")
        assertEquals("tok_1", DeepLinks.pendingShareToken)
        assertEquals("https://example.org/music", DeepLinks.linkServer)
        assertNull(DeepLinks.rejectForeign("https://EXAMPLE.org:443/music/"))
        assertEquals("tok_1", DeepLinks.consumeShareToken())
    }

    @Test
    fun dropsLinksForAnotherServer() {
        reset()
        DeepLinks.handle("https://a.example/listen/XYZ")
        assertEquals("a.example", DeepLinks.rejectForeign("https://b.example"))
        assertNull(DeepLinks.pendingListenCode)
        assertNull(DeepLinks.linkServer)
    }

    @Test
    fun invitesNameTheirServer() {
        reset()
        DeepLinks.handle("cornspies-music://open?url=http%3A%2F%2F192.168.1.5%3A3001%2Fregister%3Finvite%3Dabc")
        assertEquals("abc", DeepLinks.consumeInvite())
        assertEquals("http://192.168.1.5:3001", DeepLinks.linkServer)
    }

    @Test
    fun ignoresUnrelatedLinks() {
        reset()
        DeepLinks.handle("cornspies-music://nowplaying")
        DeepLinks.handle("cornspies-music://open?url=javascript%3Aalert(1)")
        assertNull(DeepLinks.linkServer)
        assertNull(DeepLinks.pendingPlaylistId)
    }
}
