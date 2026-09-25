package com.truvity.example.stat

import com.connectrpc.ProtocolClientConfig
import io.opentelemetry.api.GlobalOpenTelemetry
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
// why). GlobalOpenTelemetry is what the starter DOES publish for code
// outside that graph, turned on in the same file.
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
