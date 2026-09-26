package com.truvity.example.stat

import io.opentelemetry.api.common.AttributeKey
import io.opentelemetry.api.common.Attributes
import io.opentelemetry.sdk.common.CompletableResultCode
import io.opentelemetry.sdk.trace.SdkTracerProvider
import io.opentelemetry.sdk.trace.data.SpanData
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor
import io.opentelemetry.sdk.trace.export.SpanExporter
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/** A double that keeps whatever it was asked to export, for reading back in a test. */
private class RecordingSpanExporter : SpanExporter {
    val exported = mutableListOf<SpanData>()

    override fun export(spans: Collection<SpanData>): CompletableResultCode {
        exported.addAll(spans)
        return CompletableResultCode.ofSuccess()
    }

    override fun flush(): CompletableResultCode = CompletableResultCode.ofSuccess()

    override fun shutdown(): CompletableResultCode = CompletableResultCode.ofSuccess()
}

private val ROUTE = AttributeKey.stringKey("http.route")
private val URL_PATH = AttributeKey.stringKey("url.path")
private val ARCHIVE_KEY = AttributeKey.stringKey("archive.key")

/** The span-attribute allow-list: an attribute nobody listed is ABSENT. */
class SpanAttributeAllowlistTest {
    private fun exportOneSpan(
        allowed: Set<String>,
        attributes: Attributes,
    ): Attributes {
        val recorder = RecordingSpanExporter()
        val provider =
            SdkTracerProvider
                .builder()
                .addSpanProcessor(SimpleSpanProcessor.create(FilteringSpanExporter(recorder, allowed)))
                .build()
        try {
            provider.get("test").spanBuilder("span").setAllAttributes(attributes).startSpan().end()
        } finally {
            provider.shutdown()
        }
        return recorder.exported.single().attributes
    }

    @Test
    fun `an unlisted attribute is absent from what exports`() {
        val got = exportOneSpan(DEFAULT_ALLOWED_ATTRIBUTES, Attributes.of(URL_PATH, "/users/42"))

        assertNull(got.get(URL_PATH))
    }

    @Test
    fun `a default allowed attribute survives`() {
        val got = exportOneSpan(DEFAULT_ALLOWED_ATTRIBUTES, Attributes.of(ROUTE, "/users/:id"))

        assertEquals("/users/:id", got.get(ROUTE))
    }

    @Test
    fun `a caller extension survives without giving up a default`() {
        val allowed = DEFAULT_ALLOWED_ATTRIBUTES + "archive.key"

        val got =
            exportOneSpan(
                allowed,
                Attributes
                    .builder()
                    .put(ROUTE, "/users/:id")
                    .put(ARCHIVE_KEY, "2026/09/26/00001.ndjson")
                    .put(URL_PATH, "/should/not/survive")
                    .build(),
            )

        assertEquals("/users/:id", got.get(ROUTE))
        assertEquals("2026/09/26/00001.ndjson", got.get(ARCHIVE_KEY))
        assertNull(got.get(URL_PATH))
    }
}
