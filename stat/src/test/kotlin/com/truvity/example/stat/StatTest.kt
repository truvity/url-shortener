package com.truvity.example.stat

import com.fasterxml.jackson.databind.ObjectMapper
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
