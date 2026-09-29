package com.truvity.example.stat

import java.io.ByteArrayInputStream
import java.net.Socket
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyFactory
import java.security.KeyStore
import java.security.Principal
import java.security.PrivateKey
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.PKCS8EncodedKeySpec
import java.util.Base64
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.SSLEngine
import javax.net.ssl.X509ExtendedKeyManager
import javax.net.ssl.X509ExtendedTrustManager
import javax.net.ssl.X509TrustManager

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

        // The general-name type of a URI in a subjectAlternativeNames entry.
        private const val URI_NAME = 6

        // The bit positions of keyCertSign and cRLSign in a keyUsage array.
        private const val KEY_CERT_SIGN = 5
        private const val CRL_SIGN = 6

        /** Every URI name in the certificate, of any scheme. */
        private fun uriNames(certificate: X509Certificate): List<String> =
            certificate.subjectAlternativeNames
                ?.filter { it.size == 2 && it[0] == URI_NAME }
                ?.map { it[1].toString() }
                ?: emptyList()

        /**
         * Why this is not the shape of a workload identity leaf, or null.
         *
         * Two rules of the X509-SVID specification. A leaf is not a
         * certificate authority and may not sign certificates or revocation
         * lists; and it carries exactly one URI name, of any scheme.
         */
        fun shapeRefusal(certificate: X509Certificate): String? {
            if (certificate.basicConstraints != -1) {
                return "the peer's certificate is a certificate authority, not a workload identity"
            }
            val usage = certificate.keyUsage
            if (usage != null && ((usage.size > KEY_CERT_SIGN && usage[KEY_CERT_SIGN]) || (usage.size > CRL_SIGN && usage[CRL_SIGN]))) {
                return "the peer's certificate may sign certificates, which a workload identity may not"
            }
            val count = uriNames(certificate).size
            if (count > 1) return "the peer's certificate carries $count URI names, and an identity is exactly one"
            return null
        }

        /** `spiffe://<trust domain>/ns/<namespace>/sa/<account>`, or null. */
        fun identityOf(certificate: X509Certificate): Pair<String, String>? {
            val uri = uriNames(certificate).singleOrNull() ?: return null
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

    // NOTE: the peer is admitted or refused INSIDE the handshake, by the
    // trust manager below, and not by an interceptor on the client. An
    // interceptor runs around the exchange: it can look at the answer only
    // after the request has been sent (and, for an application interceptor,
    // cannot see the connection at all), so it refused every answer while
    // the peer had already acted on the request.

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
        val chainTrust = trustFactory.trustManagers.filterIsInstance<X509TrustManager>().first()
        val trust = PeerTrustManager(chainTrust, trustDomain, peers)

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

/**
 * Verifies the chain, then admits the peer by the ACCOUNT in its certificate.
 *
 * Both halves run during the handshake, so a peer that is not admitted is
 * refused before a single request byte is sent to it. The chain is checked
 * by the delegate; this is the other half — WHO the verified certificate
 * belongs to. A client that checked only the chain would accept any workload
 * in the trust domain that happened to answer on that address.
 */
internal class PeerTrustManager(
    private val delegate: X509TrustManager,
    private val trustDomain: String,
    private val peers: Set<String>,
) : X509ExtendedTrustManager() {
    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?, socket: Socket?) {
        (delegate as? X509ExtendedTrustManager)?.checkServerTrusted(chain, authType, socket)
            ?: delegate.checkServerTrusted(chain, authType)
        admit(chain)
    }

    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?, engine: SSLEngine?) {
        (delegate as? X509ExtendedTrustManager)?.checkServerTrusted(chain, authType, engine)
            ?: delegate.checkServerTrusted(chain, authType)
        admit(chain)
    }

    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
        delegate.checkServerTrusted(chain, authType)
        admit(chain)
    }

    // This component serves nothing, so a client certificate is never
    // checked here; if it ever did, the account list is about who ANSWERS.
    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?, socket: Socket?) =
        checkClientTrusted(chain, authType)

    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?, engine: SSLEngine?) =
        checkClientTrusted(chain, authType)

    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) =
        delegate.checkClientTrusted(chain, authType)

    override fun getAcceptedIssuers(): Array<X509Certificate> = delegate.acceptedIssuers

    private fun admit(chain: Array<out X509Certificate>?) {
        val leaf =
            chain?.firstOrNull()
                ?: throw CertificateException("the peer presented no certificate")
        Identity.shapeRefusal(leaf)?.let { throw CertificateException(it) }
        val (domain, account) =
            Identity.identityOf(leaf)
                ?: throw CertificateException("the peer's certificate carries no workload identity")
        if (domain != trustDomain) {
            throw CertificateException("refused a peer: $account is from trust domain $domain, not $trustDomain")
        }
        if (account !in peers) {
            throw CertificateException("refused a peer: $account is not one this component accepts an answer from")
        }
    }
}
