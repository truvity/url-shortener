package com.truvity.example.stat

import com.sun.net.httpserver.HttpsConfigurator
import com.sun.net.httpserver.HttpsParameters
import com.sun.net.httpserver.HttpsServer
import io.opentelemetry.api.OpenTelemetry
import java.io.IOException
import java.net.InetSocketAddress
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyStore
import java.security.cert.X509Certificate
import java.util.Base64
import java.util.concurrent.atomic.AtomicInteger
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

private const val DOMAIN = "example.invalid"

/** A self-signed EC identity whose only name is a SPIFFE URI. */
private class Pair(
    val dir: Path,
    val name: String,
    val account: String,
    /** Further keytool `-ext` arguments, and the SAN list, for a certificate a platform would never mint. */
    extra: List<String> = emptyList(),
    sans: String = "uri:spiffe://$DOMAIN/ns/shortener/sa/$account",
) {
    val store: KeyStore

    init {
        val file = dir.resolve("$name.p12")
        val keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString()
        val process =
            ProcessBuilder(
                keytool, "-genkeypair", "-alias", "identity", "-keyalg", "EC", "-groupname", "secp256r1",
                "-dname", "CN=$name", "-validity", "1",
                "-ext", "san=$sans",
                "-ext", "eku=serverAuth,clientAuth",
                *extra.toTypedArray(),
                "-keystore", file.toString(), "-storetype", "PKCS12", "-storepass", "changeit",
            ).redirectErrorStream(true).start()
        val out = process.inputStream.readBytes().decodeToString()
        check(process.waitFor() == 0) { "keytool failed: $out" }
        store = KeyStore.getInstance("PKCS12").apply { Files.newInputStream(file).use { load(it, "changeit".toCharArray()) } }
    }

    val certificate: X509Certificate get() = store.getCertificate("identity") as X509Certificate

    /** The three files the platform mounts, with this identity as the client's own and [root] as the CA. */
    fun mount(root: Pair): Tls {
        fun pem(label: String, der: ByteArray) =
            "-----BEGIN $label-----\n" + Base64.getMimeEncoder(64, "\n".toByteArray()).encodeToString(der) + "\n-----END $label-----\n"
        val cert = dir.resolve("$name.crt").also { Files.writeString(it, pem("CERTIFICATE", certificate.encoded)) }
        val key =
            dir.resolve("$name.key").also {
                Files.writeString(it, pem("PRIVATE KEY", store.getKey("identity", "changeit".toCharArray()).encoded))
            }
        val ca = dir.resolve("$name.ca").also { Files.writeString(it, pem("CERTIFICATE", root.certificate.encoded)) }
        return Tls(
            mode = "strict",
            certFile = cert.toString(),
            keyFile = key.toString(),
            caFile = ca.toString(),
            trustDomain = DOMAIN,
            peers = listOf(Peer("shortener", "urls")),
        )
    }
}

/** An HTTPS server that REQUIRES a client certificate and counts the requests that reach it. */
private class Server(server: Pair, client: Pair) : AutoCloseable {
    val requests = AtomicInteger()
    private val https: HttpsServer = HttpsServer.create(InetSocketAddress("127.0.0.1", 0), 0)
    val port: Int get() = https.address.port

    init {
        val keys = KeyManagerFactory.getInstance("SunX509").apply { init(server.store, "changeit".toCharArray()) }
        val trust = KeyStore.getInstance("PKCS12").apply { load(null, null); setCertificateEntry("client", client.certificate) }
        val trusts = TrustManagerFactory.getInstance("PKIX").apply { init(trust) }
        val context = SSLContext.getInstance("TLSv1.3").apply { init(keys.keyManagers, trusts.trustManagers, null) }
        https.httpsConfigurator =
            object : HttpsConfigurator(context) {
                override fun configure(params: HttpsParameters) {
                    params.needClientAuth = true
                }
            }
        https.createContext("/") { exchange ->
            requests.incrementAndGet()
            exchange.sendResponseHeaders(200, -1)
            exchange.close()
        }
        https.start()
    }

    override fun close() = https.stop(0)
}

class IdentityHandshakeTest {
    private fun call(tls: Tls, port: Int): Int {
        val client = instrument(urlsHttpClientBuilder(tls).build(), OpenTelemetry.noop())
        return client.newCall(okhttp3.Request.Builder().url("https://127.0.0.1:$port/").build()).execute().use { it.code }
    }

    @Test
    fun `a call to an admitted peer succeeds and reaches the server exactly once`() {
        val dir = Files.createTempDirectory("stat-identity")
        val me = Pair(dir, "me", "stat")
        val urls = Pair(dir, "urls", "urls")
        Server(urls, me).use { server ->
            // One request in, one response out, no exception: what a
            // consumer needs to acknowledge the message once and only once.
            assertEquals(200, call(me.mount(urls), server.port))
            assertEquals(1, server.requests.get())
        }
    }

    @Test
    fun `a peer with another workload identity is refused at the handshake`() {
        val dir = Files.createTempDirectory("stat-identity")
        val me = Pair(dir, "me", "stat")
        val impostor = Pair(dir, "impostor", "somebody-else")
        Server(impostor, me).use { server ->
            assertFailsWith<IOException> { call(me.mount(impostor), server.port) }
            // Refused BEFORE any request bytes: the server must have seen nothing.
            assertEquals(0, server.requests.get())
        }
    }

    private fun refused(server: Pair, me: Pair, why: String) {
        Server(server, me).use { s ->
            val failure = assertFailsWith<IOException> { call(me.mount(server), s.port) }
            // The reason is ours, not a chain failure: the certificate below verifies.
            assertTrue(generateSequence<Throwable>(failure) { it.cause }.any { it.message?.contains(why) == true }, "no '$why' in $failure")
            assertEquals(0, s.requests.get())
        }
    }

    @Test
    fun `a peer whose leaf is a certificate authority is refused`() {
        val dir = Files.createTempDirectory("stat-identity")
        val me = Pair(dir, "me", "stat")
        val ca = Pair(dir, "ca", "urls", extra = listOf("-ext", "bc=ca:true"))
        refused(ca, me, "is a certificate authority")
    }

    @Test
    fun `a peer whose leaf may sign certificates is refused`() {
        val dir = Files.createTempDirectory("stat-identity")
        val me = Pair(dir, "me", "stat")
        val signer = Pair(dir, "signer", "urls", extra = listOf("-ext", "ku=digitalSignature,keyCertSign"))
        refused(signer, me, "may sign certificates")
    }

    @Test
    fun `a peer whose leaf carries two URI names is refused`() {
        val dir = Files.createTempDirectory("stat-identity")
        val me = Pair(dir, "me", "stat")
        val two =
            Pair(dir, "two", "urls", sans = "uri:spiffe://$DOMAIN/ns/shortener/sa/urls,uri:https://example.org/other")
        refused(two, me, "2 URI names")
    }
}
