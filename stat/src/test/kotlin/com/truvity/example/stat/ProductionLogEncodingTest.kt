package com.truvity.example.stat

import ch.qos.logback.classic.LoggerContext
import ch.qos.logback.classic.joran.JoranConfigurator
import ch.qos.logback.classic.util.LogbackMDCAdapter
import com.fasterxml.jackson.databind.ObjectMapper
import io.opentelemetry.sdk.trace.SdkTracerProvider
import org.springframework.core.env.Environment
import org.springframework.core.env.StandardEnvironment
import java.io.ByteArrayOutputStream
import java.io.PrintStream
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

// Proves the chain this service actually ships, not a stand-in for it: the
// REAL logback-spring.xml -- OpenTelemetryAppender wrapping
// StructuredLogEncoder with format "ecs" -- loaded into its own
// LoggerContext, so this does not disturb the JVM's real logging or any
// other test's.
//
// A ListAppender test (see LogTraceCorrelationTest) proves the appender
// puts trace_id/span_id into the MDC. It does NOT prove what the log store
// actually reads, because Spring Boot's ECS formatter has its own rules for
// turning an MDC entry into JSON and they are not "every key verbatim" for
// every key shape (a dotted key nests). The only way to know what this
// service's log line looks like is to run the real encoder and read the
// bytes it wrote -- which is what this test does.
class ProductionLogEncodingTest {
    private val mapper = ObjectMapper()
    private lateinit var context: LoggerContext
    private lateinit var originalErr: PrintStream
    private lateinit var captured: ByteArrayOutputStream

    @BeforeTest
    fun wireTheRealConfiguration() {
        // The swap has to happen BEFORE the configuration is loaded: a
        // ConsoleAppender resolves System.err to a concrete stream once, at
        // start() time, and logback-spring.xml's STDERR appender starts as
        // soon as this configuration is applied.
        originalErr = System.err
        captured = ByteArrayOutputStream()
        System.setErr(PrintStream(captured, true, Charsets.UTF_8))

        context = LoggerContext()
        // A bare LoggerContext() has no MDCAdapter: the normal boot path
        // (ContextInitializer for the JVM's default context, or Spring's
        // LogbackLoggingSystem for this application) sets one, but nobody
        // does that for a context built by hand. Without it
        // OpenTelemetryAppender's MDC.getCopyOfContextMap() NPEs on a null
        // adapter -- found via the status log entry it leaves behind, which
        // getLogger("correlation").info(...) swallows unless you look.
        context.setMDCAdapter(LogbackMDCAdapter())
        // What LogbackLoggingSystem does before it loads this same file when
        // the real application starts: StructuredLogEncoder looks this up by
        // key and refuses to start without it (Assert.state, not a default).
        context.putObject(Environment::class.java.name, StandardEnvironment())
        val configurator = JoranConfigurator()
        configurator.context = context
        val resource =
            checkNotNull(javaClass.getResource("/logback-spring.xml")) {
                "logback-spring.xml is not on the test classpath -- it ships in src/main/resources"
            }
        configurator.doConfigure(resource)
    }

    @AfterTest
    fun restore() {
        context.stop()
        System.setErr(originalErr)
    }

    @Suppress("UNCHECKED_CAST")
    private fun loggedEvent(): Map<String, Any?> {
        System.err.flush()
        val line = captured.toString(Charsets.UTF_8).lineSequence().single { it.isNotBlank() }
        return mapper.readValue(line, Map::class.java) as Map<String, Any?>
    }

    @Test
    fun `the production encoder emits trace_id and span_id as top-level fields`() {
        val tracer = SdkTracerProvider.builder().build().get("test")
        val span = tracer.spanBuilder("request").startSpan()
        span.makeCurrent().use {
            context.getLogger("correlation").info("handled")
        }
        span.end()

        val event = loggedEvent()
        assertEquals(
            span.spanContext.traceId,
            event["trace_id"],
            "trace_id was not a top-level string field of what the ECS encoder wrote: $event",
        )
        assertEquals(
            span.spanContext.spanId,
            event["span_id"],
            "span_id was not a top-level string field of what the ECS encoder wrote: $event",
        )
    }

    @Test
    fun `with no span current the production encoder writes neither field`() {
        context.getLogger("correlation").info("handled")

        val event = loggedEvent()
        assertFalse(event.containsKey("trace_id"), "no span was current: trace_id must be absent: $event")
        assertFalse(event.containsKey("span_id"), "no span was current: span_id must be absent: $event")
    }
}
