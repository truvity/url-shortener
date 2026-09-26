package com.truvity.example.stat

import io.opentelemetry.api.common.AttributeKey
import io.opentelemetry.api.common.Attributes
import io.opentelemetry.api.common.AttributesBuilder
import io.opentelemetry.sdk.autoconfigure.spi.AutoConfigurationCustomizerProvider
import io.opentelemetry.sdk.common.CompletableResultCode
import io.opentelemetry.sdk.trace.data.DelegatingSpanData
import io.opentelemetry.sdk.trace.data.SpanData
import io.opentelemetry.sdk.trace.export.SpanExporter
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

/**
 * The span-attribute allow-list: an attribute nobody thought about is
 * ABSENT, not exported because some instrumentation library happened to
 * add it. OpenTelemetry semantic-convention keys that describe a call's
 * shape rather than its content -- never grows to accommodate one caller,
 * which is why this component uses it unchanged (it adds none of its own;
 * see [Stat.kt][com.truvity.example.stat] for the `messaging.*` keys it
 * sets, all of which are already here).
 *
 * Deliberately NOT here: url.path, url.query, url.full (the request line
 * itself -- ids, search terms, tokens), any header, any database or
 * messaging PAYLOAD, and any peer address. Those are exactly the
 * attributes an instrumentation library adds on its own, which is why this
 * is an ALLOW list rather than a set of things to strip.
 */
val DEFAULT_ALLOWED_ATTRIBUTES: Set<String> =
    setOf(
        "http.request.method",
        "http.route",
        "http.response.status_code",
        "rpc.system",
        "rpc.service",
        "rpc.method",
        "rpc.grpc.status_code",
        "rpc.connect_rpc.error_code",
        "db.system",
        "db.system.name",
        "db.operation",
        "db.operation.name",
        "db.query.text",
        "db.response.returned_rows",
        "messaging.system",
        "messaging.destination.name",
        "messaging.operation",
        "messaging.operation.type",
        "server.port",
        "error.type",
        "otel.status_code",
        "otel.status_description",
    )

/**
 * A [SpanData] with its attributes replaced, everything else read through
 * to [original].
 */
internal class FilteredSpanData(
    private val original: SpanData,
    private val allowed: Set<String>,
) : DelegatingSpanData(original) {
    override fun getAttributes(): Attributes {
        val builder: AttributesBuilder = Attributes.builder()
        original.attributes.forEach { key, value -> if (allowed.contains(key.key)) putRaw(builder, key, value) }
        return builder.build()
    }

    // AttributeKey erases its value type to `AttributeKey<*>` once it comes
    // back out of `Attributes.forEach`. The value at runtime always matches
    // the key it was stored under -- that invariant is `Attributes`'s own --
    // so casting the KEY to `AttributeKey<Any>` (rather than leaving `T`
    // for the compiler to infer, which is ambiguous between this overload
    // and the Long/Int specialisation) is a safe, narrow unchecked cast
    // rather than a widened one.
    private fun putRaw(
        builder: AttributesBuilder,
        key: AttributeKey<*>,
        value: Any,
    ) {
        @Suppress("UNCHECKED_CAST")
        val typedKey = key as AttributeKey<Any>
        builder.put(typedKey, value)
    }
}

/**
 * Wraps a span exporter so no attribute outside [allowed] reaches it.
 *
 * The SDK hands a `SpanProcessor.onEnd` an already-immutable [SpanData], so
 * there is no processor hook that lets code remove an attribute
 * instrumentation added earlier in the span's life. This wraps the
 * EXPORTER instead: the last point before spans leave the process where
 * the shape is still ours to change.
 */
internal class FilteringSpanExporter(
    private val delegate: SpanExporter,
    private val allowed: Set<String>,
) : SpanExporter {
    override fun export(spans: Collection<SpanData>): CompletableResultCode =
        delegate.export(spans.map { FilteredSpanData(it, allowed) })

    override fun flush(): CompletableResultCode = delegate.flush()

    override fun shutdown(): CompletableResultCode = delegate.shutdown()
}

/**
 * Installs the allow-list on the span exporter the Spring Boot starter
 * builds from OpenTelemetry's own environment.
 *
 * `AutoConfigurationCustomizerProvider` is the starter's own extension
 * point: its component loader collects beans of this type from the
 * application context (alongside anything on the classpath's SPI), which
 * is what makes a `@Bean` -- rather than a `META-INF/services` entry -- the
 * idiomatic way to reach it from a Spring service.
 */
@Configuration
class TelemetryConfig {
    @Bean
    fun spanAttributeAllowlist(): AutoConfigurationCustomizerProvider =
        AutoConfigurationCustomizerProvider { customizer ->
            customizer.addSpanExporterCustomizer { exporter, _ ->
                FilteringSpanExporter(exporter, DEFAULT_ALLOWED_ATTRIBUTES)
            }
        }
}
