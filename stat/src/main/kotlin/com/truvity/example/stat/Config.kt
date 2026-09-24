package com.truvity.example.stat

import com.fasterxml.jackson.databind.JsonNode
import com.truvity.policy.load

/**
 * What this binary reads, and the shape it reads it into.
 *
 * One file, one schema, validated before anything is constructed — the same
 * rule the Go and Python components follow, using the same loader against
 * the same schema the chart's tests validate against.
 *
 * Data classes rather than the framework's own configuration binding. The
 * framework would read a file too, and a second description of the same
 * shape is the thing the configuration contract exists to prevent: the
 * schema is the description, and this decodes what the schema accepted.
 */
data class Probes(val address: String)

data class Consumer(val stream: String, val durable: String, val subject: String?)

data class Nats(val url: String, val tokenFile: String?)

data class Events(val nats: Nats, val consumer: Consumer)

data class Peer(val namespace: String, val serviceAccount: String)

data class Tls(
    val mode: String,
    val certFile: String?,
    val keyFile: String?,
    val caFile: String?,
    val trustDomain: String?,
    val peers: List<Peer> = emptyList(),
)

data class Config(
    val probes: Probes,
    val logLevel: String,
    val drainSeconds: Int,
    val urlsAddress: String,
    val events: Events,
    val tls: Tls?,
)

/** The schema this binary validates against, carried in the jar beside it. */
const val SCHEMA_RESOURCE: String = "/stat.schema.json"

private fun schema(): String =
    object {}.javaClass.getResourceAsStream(SCHEMA_RESOURCE)?.use { it.readBytes().decodeToString() }
        ?: error("schema $SCHEMA_RESOURCE is not carried by this build")

/** Loads and validates the configuration file, or throws naming the key. */
fun readConfig(path: String): Config {
    val doc = load(path, schema())
    return Config(
        probes = Probes(doc.req("/probes/address")),
        logLevel = doc.at("/log/level").asText("info"),
        drainSeconds = doc.at("/drain/seconds").asInt(20),
        urlsAddress = doc.req("/urls/address"),
        events =
            Events(
                nats = Nats(doc.req("/events/nats/url"), doc.opt("/events/nats/tokenFile")),
                consumer =
                    Consumer(
                        stream = doc.req("/events/consumer/stream"),
                        durable = doc.req("/events/consumer/durable"),
                        subject = doc.opt("/events/consumer/subject"),
                    ),
            ),
        tls = doc.at("/tls").takeIf { !it.isMissingNode }?.toTls(),
    )
}

private fun JsonNode.toTls() =
    Tls(
        mode = at("/mode").asText("off"),
        certFile = opt("/certFile"),
        keyFile = opt("/keyFile"),
        caFile = opt("/caFile"),
        trustDomain = opt("/trustDomain"),
        peers =
            at("/peers").map {
                Peer(it.at("/namespace").asText(), it.at("/serviceAccount").asText())
            },
    )

// The schema has already refused a document missing a required key, so a
// missing value here is a bug in this file rather than in the file being
// read — which is why it fails loudly instead of defaulting.
private fun JsonNode.req(pointer: String): String =
    at(pointer).takeIf { !it.isMissingNode && !it.isNull }?.asText()
        ?: error("$pointer passed validation and is not readable: the schema and this decoder disagree")

private fun JsonNode.opt(pointer: String): String? =
    at(pointer).takeIf { !it.isMissingNode && !it.isNull }?.asText()
