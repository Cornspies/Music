package com.music.app

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class WhatsNewTest {

    private val notes = mapOf("2.0.0" to listOf("A new thing"), "2.1.0" to emptyList())

    @Test
    fun shownOnceAfterAnUpdate() {
        assertEquals(listOf("A new thing"), whatsNew(seen = "1.9.0", current = "2.0.0", signedIn = true, notes = notes))
        assertNull(whatsNew(seen = "2.0.0", current = "2.0.0", signedIn = true, notes = notes))
    }

    @Test
    fun notShownOnAFirstStart() {
        assertNull(whatsNew(seen = null, current = "2.0.0", signedIn = false, notes = notes))
    }

    @Test
    fun shownToAnInstallFromBeforeTheNoteExisted() {
        assertEquals(listOf("A new thing"), whatsNew(seen = null, current = "2.0.0", signedIn = true, notes = notes))
    }

    @Test
    fun aVersionWithoutPointsShowsNothing() {
        assertNull(whatsNew(seen = "2.0.0", current = "2.1.0", signedIn = true, notes = notes))
        assertNull(whatsNew(seen = "2.0.0", current = "3.0.0", signedIn = true, notes = notes))
        assertNull(whatsNew(seen = "2.0.0", current = null, signedIn = true, notes = notes))
    }
}
