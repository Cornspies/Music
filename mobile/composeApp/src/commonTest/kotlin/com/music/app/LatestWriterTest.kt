package com.music.app

import com.music.app.offline.LatestWriter
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlin.test.Test
import kotlin.test.assertEquals

class LatestWriterTest {

    private fun writing(test: suspend (CoroutineScope) -> Unit): Unit = runBlocking {
        val scope = CoroutineScope(Dispatchers.Default)
        try {
            withTimeout(10_000) { test(scope) }
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun writesInOrderAndSkipsToTheNewest() = writing { scope ->
        val written = mutableListOf<Int>()
        val firstStarted = CompletableDeferred<Unit>()
        val firstMayEnd = CompletableDeferred<Unit>()
        val writer = LatestWriter<Int>(scope) { value ->
            if (value == 1) {
                firstStarted.complete(Unit)
                firstMayEnd.await()
            }
            written += value
        }

        writer.submit(1)
        firstStarted.await()
        for (value in 2..5) writer.submit(value)
        firstMayEnd.complete(Unit)
        writer.flush()

        assertEquals(listOf(1, 5), written)
    }

    @Test
    fun flushWaitsUntilTheValueIsWritten() = writing { scope ->
        val written = mutableListOf<Int>()
        val writer = LatestWriter<Int>(scope) { value ->
            delay(50)
            written += value
        }

        writer.flush()
        assertEquals(emptyList(), written)

        writer.submit(1)
        writer.flush()
        assertEquals(listOf(1), written)

        writer.submit(2)
        writer.submit(3)
        writer.flush()
        assertEquals(3, written.last())
    }

    @Test
    fun aFailedWriteDoesNotStopTheNextOne() = writing { scope ->
        val written = mutableListOf<Int>()
        val writer = LatestWriter<Int>(scope) { value ->
            if (value == 1) error("disk full")
            written += value
        }

        writer.submit(1)
        writer.flush()
        writer.submit(2)
        writer.flush()

        assertEquals(listOf(2), written)
    }
}
