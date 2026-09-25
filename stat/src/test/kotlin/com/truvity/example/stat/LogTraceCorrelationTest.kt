package com.truvity.example.stat

import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.read.ListAppender
import io.opentelemetry.instrumentation.logback.mdc.v1_0.OpenTelemetryAppender
import io.opentelemetry.sdk.trace.SdkTracerProvider
import org.slf4j.LoggerFactory
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

// Proves the wiring logback-spring.xml declares by class name only: the
// OpenTelemetryAppender that wraps STDERR there wraps a ListAppender here,
// via logback-test.xml, so the MDC map a line carried can be read directly.
class LogTraceCorrelationTest {
    private val logger = LoggerFactory.getLogger("correlation-test") as ch.qos.logback.classic.Logger

    @Suppress("UNCHECKED_CAST")
    private fun captured(): ListAppender<ILoggingEvent> {
        val otel = logger.getAppender("OTEL") as OpenTelemetryAppender
        return otel.getAppender("LIST") as ListAppender<ILoggingEvent>
    }

    @BeforeTest
    fun clear() {
        captured().list.clear()
    }

    @Test
    fun `a line logged inside a span carries its ids`() {
        val tracer = SdkTracerProvider.builder().build().get("test")
        val span = tracer.spanBuilder("request").startSpan()
        span.makeCurrent().use { logger.info("handled") }
        span.end()

        val event = captured().list.single()
        assertEquals(span.spanContext.traceId, event.mdcPropertyMap["trace_id"], "trace_id did not match the current span")
        assertEquals(span.spanContext.spanId, event.mdcPropertyMap["span_id"], "span_id did not match the current span")
    }

    @Test
    fun `a line logged with no span current carries neither field`() {
        logger.info("handled")

        val event = captured().list.single()
        assertNull(event.mdcPropertyMap["trace_id"], "no span was current: trace_id must be absent, not present")
        assertNull(event.mdcPropertyMap["span_id"], "no span was current: span_id must be absent, not present")
    }
}
