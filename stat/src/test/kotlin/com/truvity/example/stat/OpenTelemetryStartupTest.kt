package com.truvity.example.stat

import io.opentelemetry.api.OpenTelemetry
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.autoconfigure.EnableAutoConfiguration
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.context.annotation.Configuration
import kotlin.test.Test
import kotlin.test.assertNotNull

// The OpenTelemetry Spring Boot starter is compiled against ONE release of
// the API and SDK, and the Spring Boot BOM manages `opentelemetry-bom` at an
// older one (see build.gradle.kts). A mismatch is not a build failure and not
// a failing unit test: it is a NoClassDefFoundError out of
// `autoConfiguredOpenTelemetrySdk` when the application context starts, which
// is the pod crash-looping on its first start. This test starts a context with
// the starter's auto-configuration and the exporters set the way a deployment
// with no endpoint sets them (`OTEL_TRACES_EXPORTER=none`), so that bean, and
// the web filter that depends on it, are instantiated by the build.
//
// The service's own beans are not loaded: they want a broker. The starter's
// beans are the subject here, and they do not depend on the service's.
@SpringBootTest(
    classes = [OpenTelemetryStartupTest.StarterOnly::class],
    properties = [
        "otel.traces.exporter=none",
        "otel.metrics.exporter=none",
        "otel.logs.exporter=none",
        // application.yaml's readiness group names the service's own `broker`
        // indicator, which is not loaded here.
        "management.endpoint.health.group.ready.include=ping",
    ],
)
class OpenTelemetryStartupTest {
    @Configuration
    @EnableAutoConfiguration
    class StarterOnly

    @Autowired lateinit var openTelemetry: OpenTelemetry

    @Test
    fun `the starter builds its SDK and the context starts`() {
        assertNotNull(openTelemetry, "the starter did not build an OpenTelemetry")
    }
}
