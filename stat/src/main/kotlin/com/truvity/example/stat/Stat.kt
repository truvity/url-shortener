package com.truvity.example.stat

import com.connectrpc.ProtocolClientConfig
import io.nats.client.impl.Headers
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.StatusCode
import io.opentelemetry.context.Context
import io.opentelemetry.context.propagation.TextMapGetter
import io.opentelemetry.instrumentation.okhttp.v3_0.OkHttpTelemetry
import com.connectrpc.extensions.GoogleJavaProtobufStrategy
import com.connectrpc.impl.ProtocolClient
import com.connectrpc.okhttp.ConnectOkHttpClient
import com.connectrpc.protocols.NetworkProtocol
import io.nats.client.Connection
import io.nats.client.Nats
import io.nats.client.Options
import io.nats.client.api.AckPolicy
import io.nats.client.api.ConsumerConfiguration
import java.nio.file.Files
import java.nio.file.Path
import java.time.Duration
import java.util.concurrent.Executors
import okhttp3.Dispatcher
import okhttp3.OkHttpClient
import okhttp3.Protocol
import org.slf4j.LoggerFactory
import urlshortener.v1.UrlsServiceClient
import urlshortener.v1.Urls.RecordClickRequest

private val log = LoggerFactory.getLogger("stat")

/** The broker connection this component consumes from. */
fun connect(nats: Nats_): Connection {
    val options =
        Options.Builder()
            .server(nats.url)
            .connectionName("url-shortener-stat")
            .apply {
                // The token is a FILE the platform mounts, never a value in
                // the configuration — and it is read on every CONNECT
                // rather than once here.
                //
                // That distinction is the whole option. The token is
                // short-lived and the runtime replaces the file in place,
                // so a client that reads it once authenticates fine until
                // its first reconnect and then fails with an authorisation
                // error naming nothing that changed. Measured: the broker
                // closed the connection sixty minutes in, the reconnect
                // presented the same expired token, and the consumer was
                // gone until somebody restarted the pod.
                nats.tokenFile?.let { file ->
                    tokenSupplier { Files.readString(Path.of(file)).trim().toCharArray() }
                }
            }
            .build()
    return Nats.connect(options)
}

/** Alias so the data class name does not collide with the client library. */
typealias Nats_ = com.truvity.example.stat.Nats

/**
 * The client for the service that owns the table.
 *
 * gRPC, over HTTP/2. Without an identity that is HTTP/2 in cleartext, which
 * OkHttp has to be told to speak — its default is HTTP/1.1 for a cleartext
 * URL, and the failure arrives as a protocol error on the first call rather
 * than as anything about configuration.
 */
// The builder, separated from urlsClient below so a test can build a
// client and look at what is actually on it -- UrlsServiceClient wraps
// connect-kotlin's own client and exposes no way to ask it.
//
// CLIENT spans for the one outbound call this service makes.
// This client is built outside Spring's bean graph -- the identity
// handshake below has to configure it directly -- so the starter's own
// instrumentation never sees it: that instruments what Spring manages,
// and manages nothing here (server.port is -1; application.yaml explains
// why). GlobalOpenTelemetry is what the starter can publish for code
// outside that graph -- but only when told to, and only before anything
// has touched it. enableGlobalOpenTelemetry() in Application.kt does that
// as the first thing main() runs, and says why it is not in the chart.
//
// `newInterceptor()` is the deprecated half of this library's API, and it
// is still the one this needs: the replacement, `newCallFactory`, wraps a
// finished OkHttpClient as a bare Call.Factory, which is not the type
// ConnectOkHttpClient's constructor takes. The interceptor is the only
// shape that fits into a Builder that is still being configured below.
@Suppress("DEPRECATION")
internal fun urlsHttpClientBuilder(tls: Tls?): OkHttpClient.Builder {
    val builder =
        OkHttpClient.Builder()
            .callTimeout(Duration.ofSeconds(10))
            // The call is made on ANOTHER thread, and the current span
            // lives in a thread-local: without this the client span is
            // created with no parent and every click starts a trace of its
            // own. Wrapping captures the context where the call is
            // ENQUEUED, which is the thread that holds the span.
            .dispatcher(
                Dispatcher(
                    Context.taskWrapping(
                        Executors.newCachedThreadPool { task ->
                            Thread(task, "urls-dispatcher").apply { isDaemon = true }
                        },
                    ),
                ),
            )
            .addInterceptor(OkHttpTelemetry.create(GlobalOpenTelemetry.get()).newInterceptor())
    val identity = Identity.load(tls)
    if (identity == null) {
        builder.protocols(listOf(Protocol.H2_PRIOR_KNOWLEDGE))
    } else {
        builder.sslSocketFactory(identity.sslSocketFactory(), identity.trustManager())
        builder.hostnameVerifier { _, _ ->
            // A platform's workload certificate carries an identity and no
            // host name, so there is no name to check. The question that
            // matters — is the thing answering the account I was told to
            // trust — is asked by the interceptor below, against the
            // certificate the handshake produced.
            true
        }
        builder.addInterceptor(identity.peerCheck())
        builder.protocols(listOf(Protocol.HTTP_2, Protocol.HTTP_1_1))
    }
    return builder
}

fun urlsClient(address: String, tls: Tls?): UrlsServiceClient {
    val config =
        ProtocolClientConfig(
            host = address,
            serializationStrategy = GoogleJavaProtobufStrategy(),
            networkProtocol = NetworkProtocol.GRPC,
        )
    return UrlsServiceClient(ProtocolClient(ConnectOkHttpClient(urlsHttpClientBuilder(tls).build()), config))
}

/**
 * Counts one click by ASKING the service that owns the table.
 *
 * The component holds no database credential at all. That is the point of
 * the boundary and not a side effect: a component that cannot write the
 * table cannot write it wrongly, and the rights it was granted stop being a
 * thing anyone has to reason about.
 */
suspend fun recordClick(client: UrlsServiceClient, longUrl: String) {
    val response =
        client.recordClick(RecordClickRequest.newBuilder().setLongUrl(longUrl).build())
    response.failure { throw IllegalStateException("record click: ${it.cause}") }
}

/** The durable consumer this component binds to. */
fun consumerConfiguration(consumer: Consumer): ConsumerConfiguration =
    ConsumerConfiguration.builder()
        .durable(consumer.durable)
        .filterSubject(consumer.subject)
        .ackPolicy(AckPolicy.Explicit)
        // Redelivery is what makes a failed call a retry rather than a lost
        // click. A click counted zero times is worse than one counted twice.
        .maxDeliver(5)
        .ackWait(Duration.ofSeconds(30))
        .build()

/** What the redirect service published, as much of it as this reads. */
data class Redirect(val urlKey: String, val longUrl: String)

/**
 * Decodes one message, or returns null for one this component does not read.
 *
 * The event's TYPE travels as a header precisely so that two kinds of event
 * on one subject stay distinguishable. A consumer that decoded the body and
 * hoped would count a click for an empty URL — which is the bug the header
 * exists to prevent, and which this repository's example had.
 */
fun decode(detailType: String?, body: String, mapper: com.fasterxml.jackson.databind.ObjectMapper): Redirect? {
    if (detailType != "URLRedirect") {
        log.warn("skipping an event this component does not read: {}", detailType)
        return null
    }
    val node = mapper.readTree(body)
    val longUrl = node.at("/long_url").asText("")
    if (longUrl.isEmpty()) {
        log.warn("skipping a redirect with no long_url")
        return null
    }
    return Redirect(node.at("/url_key").asText(""), longUrl)
}

/** Reads a trace context out of a message's headers. */
internal object NatsHeaders : TextMapGetter<Headers> {
    override fun keys(carrier: Headers): Iterable<String> = carrier.keySet()

    override fun get(carrier: Headers?, key: String): String? = carrier?.getFirst(key)
}

/**
 * Runs [block] as a span that CONTINUES the publisher's trace.
 *
 * The broker keeps the trace context the publisher put in the message, and
 * nothing else does anything with it: this is the only place it is read.
 * Without it the redirect that caused a click and the click's counting are
 * two traces, and nothing in the store says one caused the other.
 *
 * A child of the publisher, not a link, because there is one publisher and
 * one message: the span describes the same piece of work, later. The span
 * is made CURRENT for the block, which is what lets the outbound call made
 * inside it become its child in turn.
 */
internal fun <T> consumed(subject: String, headers: Headers?, block: () -> T): T {
    val otel = GlobalOpenTelemetry.get()
    val parent = otel.propagators.textMapPropagator.extract(Context.root(), headers ?: Headers(), NatsHeaders)
    val span =
        otel.getTracer("url-shortener-stat")
            .spanBuilder("$subject process")
            .setParent(parent)
            .setSpanKind(SpanKind.CONSUMER)
            .setAttribute("messaging.system", "nats")
            .setAttribute("messaging.destination.name", subject)
            .setAttribute("messaging.operation.type", "process")
            .startSpan()
    try {
        span.makeCurrent().use { return block() }
    } catch (e: Throwable) {
        span.recordException(e)
        span.setStatus(StatusCode.ERROR)
        throw e
    } finally {
        span.end()
    }
}
