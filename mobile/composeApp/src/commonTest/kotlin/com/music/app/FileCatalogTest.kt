package com.music.app

import com.music.app.offline.FileCatalog
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FileCatalogTest {

    @Test
    fun findsATracksFilesFromTheListing() {
        val catalog = FileCatalog(listOf("a.mp3", "a.flac.part", "a.cover", "ab.mp3", "b.opus"))
        assertEquals(setOf("a.mp3", "a.flac.part", "a.cover"), catalog.names("a").toSet())
        assertEquals(listOf("ab.mp3"), catalog.names("ab"))
        assertEquals(emptyList(), catalog.names("c"))
        assertTrue("b.opus" in catalog)
        assertFalse("b.mp3" in catalog)
    }

    @Test
    fun keepsARunningTotal() {
        val catalog = FileCatalog(emptyList())
        catalog.put("a.mp3", 100)
        catalog.put("a.cover", 10)
        catalog.put("b.mp3", 1)
        assertEquals(111L, catalog.bytes)

        catalog.put("a.mp3", 70)
        assertEquals(81L, catalog.bytes)

        catalog.remove("a.cover")
        catalog.remove("a.cover")
        catalog.remove("never.there")
        assertEquals(71L, catalog.bytes)
        assertEquals(listOf("a.mp3"), catalog.names("a"))

        catalog.clear()
        assertEquals(0L, catalog.bytes)
        assertFalse("b.mp3" in catalog)
    }

    @Test
    fun countsTheListedFilesOnceTheyAreMeasured() {
        val catalog = FileCatalog(listOf("a.mp3", "b.mp3", "b.flac.part"))
        assertEquals(0L, catalog.bytes)
        assertEquals(setOf("a.mp3", "b.mp3"), catalog.unmeasured().toSet())

        catalog.measured(mapOf("a.mp3" to 100L, "b.mp3" to 50L))
        assertEquals(150L, catalog.bytes)
        assertEquals(emptyList(), catalog.unmeasured())

        catalog.measured(mapOf("a.mp3" to 100L, "b.mp3" to 50L))
        assertEquals(150L, catalog.bytes)

        catalog.remove("a.mp3")
        assertEquals(50L, catalog.bytes)
    }

    @Test
    fun changesMadeWhileMeasuringStand() {
        val catalog = FileCatalog(listOf("gone.mp3", "again.mp3", "same.mp3"))
        val listed = catalog.unmeasured()

        catalog.remove("gone.mp3")
        catalog.put("again.mp3", 7)
        catalog.put("new.mp3", 5)
        catalog.measured(listed.associateWith { 30L })

        assertEquals(42L, catalog.bytes)
        assertFalse("gone.mp3" in catalog)
    }
}
