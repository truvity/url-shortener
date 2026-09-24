package com.truvity.example.stat

import java.io.ByteArrayInputStream
import java.net.Socket
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyFactory
import java.security.KeyStore
import java.security.Principal
import java.security.PrivateKey
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.PKCS8EncodedKeySpec
import java.util.Base64
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509ExtendedKeyManager
import javax.net.ssl.X509TrustManager
import okhttp3.Interceptor
import okhttp3.Response

/**
 * The mounted workload identity, as a CLIENT presents it.
 *
 * Three things, and deliberately no more: load what the platform mounted and
 * RELOAD it when the platform replaces it, present it, and admit a peer by
 * the ACCOUNT in its certificate rather than by the address it answered on.
 * It never fetches or mints a certificate — a workload that did would be
 * asserting an identity rather than presenting one it was given.
 *
 * This component serves nothing, so only the client half is here. The
 * server half on the JVM is the framework's SSL bundles, which reload on
 * update; see docs/canon/kotlin.md.
 */
class Identity(
    private val certFile: Path,
    private val keyFile: Path,
    private val caFile: Path,
    private val trustDomain: String,
    private val peers: Set<String>,
) {
    companion object {
        private const val SCHEME = "spiffe"
        private const val PATH_PARTS = 4

        /**
         * Reads the `tls` block, or returns null when there is nothing to
         * load.
         *
         * Null means "speak cleartext", not an error. `off` is the default
         * and always will be: a chart is installable by someone whose
         * platform provides none of this.
         */
        fun load(tls: Tls?): Identity? {
            if (tls == null || tls.mode == "off") return null
            val cert = requireNotNull(tls.certFile) { "tls.certFile is required once tls.mode is not off" }
            val key = requireNotNull(tls.keyFile) { "tls.keyFile is required once tls.mode is not off" }
            val ca = requireNotNull(tls.caFile) { "tls.caFile is required once tls.mode is not off" }
            val domain =
                requireNotNull(tls.trustDomain) {
                    "tls.trustDomain is required once tls.mode is not off: " +
                        "without it a peer from any trust domain is admitted"
                }
            return Identity(
                Path.of(cert),
                Path.of(key),
                Path.of(ca),
                domain,
                tls.peers.map { "${it.namespace}/${it.serviceAccount}" }.toSet(),
            )
        }

        /** `spiffe://<trust domain>/ns/<namespace>/sa/<account>`, or null. */
        fun identityOf(certificate: X509Certificate): Pair<String, String>? {
            val uri =
                certificate.subjectAlternativeNames
                    ?.firstOrNull { it.size == 2 && it[0] == 6 }
                    ?.get(1)
                    ?.toString() ?: return null
            if (!uri.startsWith("$SCHEME://")) return null
            val rest = uri.removePrefix("$SCHEME://")
            val domain = rest.substringBefore('/')
            val parts = rest.substringAfter('/').split('/')
            if (parts.size != PATH_PARTS || parts[0] != "ns" || parts[2] != "sa") return null
            return domain to "${parts[1]}/${parts[3]}"
        }
    }

    // What was loaded, and what it was loaded from. A rotation replaces the
    // files in place, so the modification time is what says it is stale.
    private data class Loaded(val stamp: Long, val context: SSLContext, val trust: X509TrustManager)

    @Volatile private var loaded: Loaded? = null

    private fun stampOf(): Long =
        listOf(certFile, keyFile, caFile).sumOf { runCatching { Files.getLastModifiedTime(it).toMillis() }.getOrDefault(0L) }

    @Synchronized
    private fun current(): Loaded {
        val stamp = stampOf()
        val held = loaded
        if (held != null && held.stamp == stamp) return held

        val certs = pemCertificates(Files.readString(certFile))
        val key = pemPrivateKey(Files.readString(keyFile))
        val roots = pemCertificates(Files.readString(caFile))

        val keyStore = KeyStore.getInstance("PKCS12").apply { load(null, CharArray(0)) }
        keyStore.setKeyEntry("identity", key, CharArray(0), certs.toTypedArray())

        val trustStore = KeyStore.getInstance("PKCS12").apply { load(null, CharArray(0)) }
        roots.forEachIndexed { i, c -> trustStore.setCertificateEntry("root-$i", c) }

        val trustFactory =
            TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply {
                init(trustStore)
            }
        val trust = trustFactory.trustManagers.filterIsInstance<X509TrustManager>().first()

        val context =
            SSLContext.getInstance("TLSv1.3").apply {
                init(arrayOf(SingleKeyManager(certs.toTypedArray(), key)), arrayOf(trust), null)
            }

        // TLS 1.3 is stated rather than inherited, and it is the same floor
        // the Go and Python packages set. Both ends of every connection here
        // are workloads this platform issued identities to, so there is
        // nothing old to be compatible with.
        val fresh = Loaded(stamp, context, trust)
        loaded = fresh
        return fresh
    }

    /**
     * The socket factory this client presents its certificate with.
     *
     * Re-reading rather than holding what start-up loaded is the point. A
     * platform rotates these on an hour or less; a process that cached the
     * first one authenticates fine until it expires and then fails
     * everywhere at once, with an error about expiry and nothing pointing at
     * the line that read it.
     */
    fun sslSocketFactory(): SSLSocketFactory = ReloadingSocketFactory { current().context.socketFactory }

    fun trustManager(): X509TrustManager = ReloadingTrustManager { current().trust }

    /**
     * Refuses a peer whose ACCOUNT is not on the list.
     *
     * The chain is verified by the handshake; this is the other half — WHO
     * the verified certificate belongs to. A client that checked only the
     * chain would accept any workload in the trust domain that happened to
     * answer on that address.
     */
    fun peerCheck(): Interceptor =
        Interceptor { chain ->
            val response: Response = chain.proceed(chain.request())
            val peerCert =
                chain.connection()?.handshake()?.peerCertificates?.firstOrNull() as? X509Certificate
                    ?: throw java.io.IOException("the peer presented no certificate")
            val identity =
                identityOf(peerCert)
                    ?: throw java.io.IOException("the peer's certificate carries no workload identity")
            val (domain, account) = identity
            if (domain != trustDomain) {
                throw java.io.IOException("refused a peer: $account is from trust domain $domain, not $trustDomain")
            }
            if (account !in peers) {
                throw java.io.IOException("refused a peer: $account is not one this component accepts an answer from")
            }
            response
        }
}

private fun pemCertificates(pem: String): List<X509Certificate> {
    val factory = CertificateFactory.getInstance("X.509")
    return Regex("-----BEGIN CERTIFICATE-----[^-]*-----END CERTIFICATE-----", RegexOption.DOT_MATCHES_ALL)
        .findAll(pem)
        .map { block ->
            factory.generateCertificate(ByteArrayInputStream(block.value.toByteArray())) as X509Certificate
        }
        .toList()
}

private fun pemPrivateKey(pem: String): PrivateKey {
    val body =
        pem.substringAfter("-----BEGIN PRIVATE KEY-----")
            .substringBefore("-----END PRIVATE KEY-----")
            .replace(Regex("\\s"), "")
    val der = Base64.getDecoder().decode(body)
    val spec = PKCS8EncodedKeySpec(der)
    // The platform decides the algorithm, not this file: an identity signed
    // with an elliptic curve key and one with RSA are both ordinary, and a
    // component that assumed either would fail on somebody else's cluster.
    for (algorithm in listOf("EC", "RSA", "EdDSA")) {
        runCatching { return KeyFactory.getInstance(algorithm).generatePrivate(spec) }
    }
    error("the mounted private key is in none of the formats this component reads")
}

/** Presents one identity, whichever alias is asked for. */
private class SingleKeyManager(
    private val chain: Array<X509Certificate>,
    private val key: PrivateKey,
) : X509ExtendedKeyManager() {
    override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(ALIAS)

    override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?) = ALIAS

    override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(ALIAS)

    override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?) = ALIAS

    override fun getCertificateChain(alias: String?) = chain

    override fun getPrivateKey(alias: String?) = key

    companion object {
        private const val ALIAS = "identity"
    }
}

/** Delegates every call to whatever the current certificate says. */
private class ReloadingSocketFactory(private val delegate: () -> SSLSocketFactory) : SSLSocketFactory() {
    override fun getDefaultCipherSuites(): Array<String> = delegate().defaultCipherSuites

    override fun getSupportedCipherSuites(): Array<String> = delegate().supportedCipherSuites

    override fun createSocket(s: Socket?, host: String?, port: Int, autoClose: Boolean) =
        harden(delegate().createSocket(s, host, port, autoClose))

    override fun createSocket(host: String?, port: Int) = harden(delegate().createSocket(host, port))

    override fun createSocket(host: String?, port: Int, localHost: java.net.InetAddress?, localPort: Int) =
        harden(delegate().createSocket(host, port, localHost, localPort))

    override fun createSocket(host: java.net.InetAddress?, port: Int) = harden(delegate().createSocket(host, port))

    override fun createSocket(
        address: java.net.InetAddress?,
        port: Int,
        localAddress: java.net.InetAddress?,
        localPort: Int,
    ) = harden(delegate().createSocket(address, port, localAddress, localPort))

    private fun harden(socket: Socket): Socket {
        (socket as? SSLSocket)?.enabledProtocols = arrayOf("TLSv1.3")
        return socket
    }
}

private class ReloadingTrustManager(private val delegate: () -> X509TrustManager) : X509TrustManager {
    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) =
        delegate().checkClientTrusted(chain, authType)

    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) =
        delegate().checkServerTrusted(chain, authType)

    override fun getAcceptedIssuers(): Array<X509Certificate> = delegate().acceptedIssuers
}
