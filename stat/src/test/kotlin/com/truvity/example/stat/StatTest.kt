package com.truvity.example.stat

import com.fasterxml.jackson.databind.ObjectMapper
import com.sun.net.httpserver.HttpServer
import io.nats.client.impl.Headers
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.api.trace.Span
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.propagation.W3CTraceContextPropagator
import io.opentelemetry.context.propagation.ContextPropagators
import io.opentelemetry.sdk.OpenTelemetrySdk
import io.opentelemetry.sdk.trace.ReadableSpan
import io.opentelemetry.sdk.trace.SdkTracerProvider
import java.net.InetSocketAddress
import okhttp3.Protocol
import okhttp3.Request
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

private val mapper = ObjectMapper()

private const val REDIRECT = """{"url_key":"abc12345","long_url":"https://example.com/x"}"""

class DecodeTest {
    @Test
    fun `a redirect event is decoded`() {
        val event = decode("URLRedirect", REDIRECT, mapper)

        assertEquals("abc12345", event?.urlKey)
        assertEquals("https://example.com/x", event?.longUrl)
    }

    @Test
    fun `an event of another kind is not decoded as this one`() {
        // The whole reason the publisher puts the type in a HEADER. Two
        // kinds of event share this stream, and a consumer that decoded the
        // body and hoped would read a request record as a redirect — which
        // is not hypothetical: this example's framework version dropped the
        // type at publish time and counted a click for an empty URL.
        assertNull(decode("URLRequest", """{"request":{"path":"/r/abc12345"}}""", mapper))
    }

    @Test
    fun `an event with no type at all is not decoded`() {
        assertNull(decode(null, REDIRECT, mapper))
    }

    @Test
    fun `a redirect with no long url is not counted`() {
        // Clicks are counted per long URL, so an empty one is never a real
        // redirect — and counting it would increment a row keyed by "".
        assertNull(decode("URLRedirect", """{"url_key":"abc12345","long_url":""}""", mapper))
    }
}

class IdentityTest {
    @Test
    fun `off loads nothing, which is not an error`() {
        // A chart's default is off, and it must produce a service that runs.
        assertNull(Identity.load(null))
        assertNull(Identity.load(Tls(mode = "off", null, null, null, null)))
    }
}

class UrlsClientTest {
    @Test
    fun `the outbound client carries a span for every call`() {
        // Built outside Spring's bean graph, so the starter's own
        // instrumentation never sees this client (it instruments what
        // Spring manages, and manages nothing here — server.port is -1).
        // Without its own interceptor this is a consumer with an outbound
        // call and no span anywhere describing it, which is exactly the
        // gap found: a tracer provider connected and exporting nothing,
        // because nothing created a span.
        val client = urlsHttpClientBuilder(null).build()
        assertTrue(
            client.interceptors.any { it.javaClass.name.startsWith("io.opentelemetry.") },
            "no OpenTelemetry interceptor on the client that makes the one outbound call this service makes",
        )
    }
}

class GlobalOpenTelemetryTest {
    @AfterTest
    fun reset() {
        System.clearProperty("otel.java.global-autoconfigure.enabled")
        GlobalOpenTelemetry.resetForTest()
    }

    @Test
    fun `once enabled the global SDK produces real spans, not no-op ones`() {
        // The claim is about what the outbound client will actually get,
        // so this asks for exactly that: a span from GlobalOpenTelemetry
        // and whether it is real. A no-op span has an invalid context, and
        // that is what the process had for the entire time it exported
        // nothing -- no error, no log, an interceptor tracing into a void.
        System.clearProperty("otel.java.global-autoconfigure.enabled")
        GlobalOpenTelemetry.resetForTest()

        enableGlobalOpenTelemetry()

        val span = GlobalOpenTelemetry.get().getTracer("test").spanBuilder("probe").startSpan()
        try {
            assertTrue(span.spanContext.isValid, "the global SDK is a no-op: nothing this service traces is recorded")
        } finally {
            span.end()
        }
    }

    @Test
    fun `an operator's explicit setting wins over the default`() {
        System.setProperty("otel.java.global-autoconfigure.enabled", "false")

        enableGlobalOpenTelemetry()

        assertEquals("false", System.getProperty("otel.java.global-autoconfigure.enabled"))
    }
}

class TraceContinuityTest {
    @AfterTest
    fun reset() {
        GlobalOpenTelemetry.resetForTest()
    }

    private fun sdk(): OpenTelemetrySdk {
        GlobalOpenTelemetry.resetForTest()
        val sdk =
            OpenTelemetrySdk.builder()
                .setTracerProvider(SdkTracerProvider.builder().build())
                .setPropagators(ContextPropagators.create(W3CTraceContextPropagator.getInstance()))
                .build()
        GlobalOpenTelemetry.set(sdk)
        return sdk
    }

    @Test
    fun `a consumed message continues the publishers trace`() {
        val sdk = sdk()
        val publisher = sdk.getTracer("test").spanBuilder("redirect").startSpan()
        val headers = Headers()
        headers.add("traceparent", "00-${publisher.spanContext.traceId}-${publisher.spanContext.spanId}-01")

        var inside: Span? = null
        consumed("events.redirect", headers) { inside = Span.current() }

        val span = inside as ReadableSpan
        assertEquals(publisher.spanContext.traceId, span.spanContext.traceId, "the consumer started a trace of its own")
        assertEquals(publisher.spanContext.spanId, span.parentSpanContext.spanId, "the consumer is not the publisher's child")
        assertEquals(SpanKind.CONSUMER, span.kind)
    }

    @Test
    fun `a message with no context still gets a span`() {
        sdk()
        var inside: Span? = null
        consumed("events.redirect", null) { inside = Span.current() }
        assertTrue(inside!!.spanContext.isValid)
    }

    @Test
    fun `the call made while consuming carries the consumers trace to the callee`() {
        val sdk = sdk()
        val seen = java.util.concurrent.atomic.AtomicReference<String?>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { exchange ->
            seen.set(exchange.requestHeaders.getFirst("traceparent"))
            exchange.sendResponseHeaders(200, -1)
            exchange.close()
        }
        server.start()
        try {
            val publisher = sdk.getTracer("test").spanBuilder("redirect").startSpan()
            val headers = Headers()
            headers.add("traceparent", "00-${publisher.spanContext.traceId}-${publisher.spanContext.spanId}-01")
            val client = urlsHttpClientBuilder(null).protocols(listOf(Protocol.HTTP_1_1)).build()

            // ENQUEUED, as the Connect client does: the call runs on a
            // dispatcher thread, and only the wrapped executor lets it see
            // the span that is current here.
            consumed("events.redirect", headers) {
                val latch = java.util.concurrent.CountDownLatch(1)
                client.newCall(Request.Builder().url("http://127.0.0.1:${server.address.port}/").build())
                    .enqueue(
                        object : okhttp3.Callback {
                            override fun onFailure(call: okhttp3.Call, e: java.io.IOException) = latch.countDown()

                            override fun onResponse(call: okhttp3.Call, response: okhttp3.Response) {
                                response.close()
                                latch.countDown()
                            }
                        },
                    )
                latch.await()
            }

            val header = seen.get()
            assertTrue(header != null, "no trace context reached the callee")
            assertTrue(
                header!!.startsWith("00-${publisher.spanContext.traceId}-"),
                "the callee joined a different trace: $header",
            )
        } finally {
            server.stop(0)
        }
    }
}
