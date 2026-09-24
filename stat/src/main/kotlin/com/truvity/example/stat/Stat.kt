package com.truvity.example.stat

import com.connectrpc.ProtocolClientConfig
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
                // the configuration.
                nats.tokenFile?.let { token(Files.readString(Path.of(it)).trim().toCharArray()) }
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
fun urlsClient(address: String, tls: Tls?): UrlsServiceClient {
    val builder = OkHttpClient.Builder().callTimeout(Duration.ofSeconds(10))
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

    val config =
        ProtocolClientConfig(
            host = address,
            serializationStrategy = GoogleJavaProtobufStrategy(),
            networkProtocol = NetworkProtocol.GRPC,
        )
    return UrlsServiceClient(ProtocolClient(ConnectOkHttpClient(builder.build()), config))
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
