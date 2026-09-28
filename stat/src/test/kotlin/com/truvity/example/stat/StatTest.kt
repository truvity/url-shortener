package com.truvity.example.stat

import com.fasterxml.jackson.databind.ObjectMapper
import com.sun.net.httpserver.HttpServer
import io.nats.client.impl.Headers
import io.opentelemetry.api.common.AttributeKey
import io.opentelemetry.api.trace.Span
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.propagation.W3CTraceContextPropagator
import io.opentelemetry.context.propagation.ContextPropagators
import io.opentelemetry.sdk.OpenTelemetrySdk
import io.opentelemetry.sdk.trace.ReadableSpan
import io.opentelemetry.sdk.trace.SdkTracerProvider
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor
import io.opentelemetry.sdk.trace.export.SpanExporter
import java.net.InetSocketAddress
import okhttp3.Protocol
import okhttp3.Request
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

/** An [OpenTelemetrySdk] wired exactly like production: real instrumentation, filtered export. */
private fun filteredSdk(recorder: SpanExporter): OpenTelemetrySdk =
    OpenTelemetrySdk.builder()
        .setTracerProvider(
            SdkTracerProvider
                .builder()
                .addSpanProcessor(SimpleSpanProcessor.create(FilteringSpanExporter(recorder, DEFAULT_ALLOWED_ATTRIBUTES)))
                .build(),
        )
        .setPropagators(ContextPropagators.create(W3CTraceContextPropagator.getInstance()))
        .build()

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
        val client = urlsHttpClientBuilder(null, filteredSdk(RecordingSpanExporter())).build()
        assertTrue(
            client.interceptors.any { it.javaClass.name.startsWith("io.opentelemetry.") },
            "no OpenTelemetry interceptor on the client that makes the one outbound call this service makes",
        )
    }

    /**
     * The regression this repository's example actually had, on a real
     * trace store: this client's own span -- the one instrumentation
     * builds for the ONE outbound call this service makes -- carried
     * `network.protocol.version`, `server.address` and `url.full`, none
     * of which are on the allow-list. Not because the allow-list was
     * wrong: because the client was built against `GlobalOpenTelemetry`,
     * a completely different, unfiltered `AutoConfiguredOpenTelemetrySdk`
     * that Spring's own SDK -- the one carrying the allow-list -- is never
     * published as.
     *
     * This drives a REAL HTTP call through the REAL OkHttp instrumentation
     * library (the same call [urlsClient] makes), behind the SAME
     * [FilteringSpanExporter] production installs (`SpanAttributeAllowlist.kt`),
     * reached the SAME way production reaches it: as the `openTelemetry`
     * this test passes to [urlsHttpClientBuilder], never through
     * `GlobalOpenTelemetry`. Before the fix this test cannot even be
     * written this way -- `urlsHttpClientBuilder` took no SDK to wire in,
     * only `GlobalOpenTelemetry.get()`, which is exactly the structural
     * gap this asserts is closed.
     */
    @Test
    fun `the outbound client span carries no attribute outside the allow-list`() {
        val recorder = RecordingSpanExporter()
        val sdk = filteredSdk(recorder)

        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { exchange ->
            exchange.sendResponseHeaders(200, -1)
            exchange.close()
        }
        server.start()
        try {
            val client = urlsHttpClientBuilder(null, sdk).protocols(listOf(Protocol.HTTP_1_1)).build()
            client
                .newCall(Request.Builder().url("http://127.0.0.1:${server.address.port}/").build())
                .execute()
                .close()
        } finally {
            server.stop(0)
        }

        val span = recorder.exported.single()
        assertEquals(SpanKind.CLIENT, span.kind)

        // The filter really ran: an allow-listed attribute the OkHttp
        // instrumentation DOES set for this call survives it.
        assertEquals("GET", span.attributes.get(AttributeKey.stringKey("http.request.method")))

        // The exact leak found on a live trace store: none of these three
        // reach the exporter, or anything else outside the allow-list --
        // the fix is structural (every attribute passes the same filter),
        // not three keys removed from what the instrumentation adds.
        for (leaked in listOf("network.protocol.version", "server.address", "url.full")) {
            assertNull(span.attributes.asMap().keys.find { it.key == leaked }, "'$leaked' reached the exporter")
        }
        val unlisted = span.attributes.asMap().keys.map { it.key }.filterNot { it in DEFAULT_ALLOWED_ATTRIBUTES }
        assertTrue(unlisted.isEmpty(), "attribute(s) outside the allow-list reached the exporter: $unlisted")
    }
}

class TraceContinuityTest {
    // No GlobalOpenTelemetry to reset between tests any more: `consumed`
    // and `urlsHttpClientBuilder` are handed this SDK directly, the same
    // way production hands them the Spring-built one.
    private fun sdk(): OpenTelemetrySdk =
        OpenTelemetrySdk.builder()
            .setTracerProvider(SdkTracerProvider.builder().build())
            .setPropagators(ContextPropagators.create(W3CTraceContextPropagator.getInstance()))
            .build()

    @Test
    fun `a consumed message continues the publishers trace`() {
        val sdk = sdk()
        val publisher = sdk.getTracer("test").spanBuilder("redirect").startSpan()
        val headers = Headers()
        headers.add("traceparent", "00-${publisher.spanContext.traceId}-${publisher.spanContext.spanId}-01")

        var inside: Span? = null
        consumed("events.redirect", headers, sdk) { inside = Span.current() }

        val span = inside as ReadableSpan
        assertEquals(publisher.spanContext.traceId, span.spanContext.traceId, "the consumer started a trace of its own")
        assertEquals(publisher.spanContext.spanId, span.parentSpanContext.spanId, "the consumer is not the publisher's child")
        assertEquals(SpanKind.CONSUMER, span.kind)
    }

    @Test
    fun `a message with no context still gets a span`() {
        val sdk = sdk()
        var inside: Span? = null
        consumed("events.redirect", null, sdk) { inside = Span.current() }
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
            val client = urlsHttpClientBuilder(null, sdk).protocols(listOf(Protocol.HTTP_1_1)).build()

            // ENQUEUED, as the Connect client does: the call runs on a
            // dispatcher thread, and only the wrapped executor lets it see
            // the span that is current here.
            consumed("events.redirect", headers, sdk) {
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
