package com.truvity.example.stat

import io.opentelemetry.api.OpenTelemetry
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlin.test.Test
import kotlin.test.assertFailsWith

/**
 * The Connect client is compiled against one kotlinx-coroutines and runs
 * against whichever the resolution picks. Spring's dependency management
 * pins its own, older one over the library's, and a mismatch is a
 * NoSuchMethodError (`Job.cancel$default`) that only fires when a call is
 * actually made -- so nothing that merely builds the client notices.
 *
 * This makes one real call, to a port nothing listens on. The call must
 * fail the way a network failure fails (an exception the caller handles),
 * and never with a LinkageError, which is what an ABI mismatch is. On a
 * mismatch the dispatcher thread dies and the call never completes, so the
 * wait is bounded: a hang is the same failure.
 */
class CoroutinesAbiTest {
    @Test
    fun `a call through the connect client links against the resolved coroutines`() {
        val client = urlsClient("http://127.0.0.1:1", null, OpenTelemetry.noop())
        val thrown =
            assertFailsWith<Throwable> {
                runBlocking { withTimeout(15_000) { recordClick(client, "https://example.com/x") } }
            }
        var cause: Throwable? = thrown
        while (cause != null) {
            check(cause !is LinkageError && cause !is TimeoutCancellationException) {
                "the client does not link against the resolved dependencies: $cause"
            }
            cause = cause.cause
        }
    }
}
