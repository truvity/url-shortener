"""Turn the ``tls`` fragment of a configuration into the contexts a service needs.

It does three things, and deliberately no more:

1. loads the certificate the platform mounted, and RELOADS it when the
   platform replaces it;
2. presents it, as a client and as a server;
3. admits a peer by the ACCOUNT it runs as, read from the identity in its
   certificate, against a list the configuration gives.

It does not fetch a certificate, mint one, or talk to an authority. That is
the platform's job, and a service that did it would be asserting an identity
rather than presenting one it was given.

The Go package of the same name implements the same three things against the
same fragment. One difference is real and is not hidden here: Go can decide
whether to admit a peer DURING the handshake, and Python cannot. Python's
``ssl`` module has no verification callback, so the chain is verified by
OpenSSL and the identity is checked immediately afterwards, by the caller,
against the certificate the handshake already produced. A connection is
therefore refused a moment later than it would be in Go — after the
handshake completes, before anything is read or written — and
:func:`verify_peer` is the call that does it. Forgetting it leaves a service
that verifies a certificate chain and admits anybody who holds one, which is
the failure that looks like success from every other angle.
"""

from __future__ import annotations

import ssl
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING, Any, Literal
from urllib.parse import urlsplit

if TYPE_CHECKING:
    from collections.abc import Iterable

__all__ = ["Identity", "Peer", "TransportError", "load"]

Mode = Literal["off", "permissive", "strict"]

# The identity's shape: spiffe://<trust domain>/ns/<namespace>/sa/<account>.
_SCHEME = "spiffe"
_PATH_LENGTH = 4

# What the shape check reads out of a DER certificate (X.509 / RFC 5280).
_OID_BASIC_CONSTRAINTS = bytes.fromhex("551d13")
_OID_KEY_USAGE = bytes.fromhex("551d0f")
_TAG_SEQUENCE = 0x30
_TAG_BOOLEAN = 0x01
_TAG_OCTET_STRING = 0x04
_TAG_BIT_STRING = 0x03
_TAG_OID = 0x06
_TAG_EXTENSIONS = 0xA3
_KEY_CERT_SIGN = 0x04  # keyUsage bit 5, in the first byte
_CRL_SIGN = 0x02  # keyUsage bit 6, in the first byte
_LONG_LENGTH = 0x80

# Stated rather than inherited, and the same floor the Go package sets.
#
# `create_default_context` already refuses anything below TLS 1.2, so this
# raises the floor by one version — but the reason to write it down is that
# the default is a property of the interpreter, and a service's transport
# floor should not change because a base image did. Both ends of every
# connection here are workloads this platform issued identities to, so there
# is nothing old to be compatible with.
_MINIMUM_VERSION = ssl.TLSVersion.TLSv1_3


def _tlv(data: bytes, offset: int) -> tuple[int, bytes, int]:
    """Read one DER element at ``offset``: its tag, its content, and where it ends."""
    try:
        tag = data[offset]
        length = data[offset + 1]
        start = offset + 2
        if length & _LONG_LENGTH:
            count = length & ~_LONG_LENGTH
            length = int.from_bytes(data[start : start + count], "big")
            start += count
    except IndexError:
        raise TransportError("the peer's certificate is malformed") from None
    end = start + length
    if end > len(data):
        raise TransportError("the peer's certificate is malformed")
    return tag, data[start:end], end


def _elements(data: bytes) -> list[tuple[int, bytes]]:
    found: list[tuple[int, bytes]] = []
    offset = 0
    while offset < len(data):
        tag, content, offset = _tlv(data, offset)
        found.append((tag, content))
    return found


def _extensions(der: bytes) -> dict[bytes, bytes]:
    """Return the certificate's extensions, by OID, each as its raw extnValue."""
    _, certificate, _ = _tlv(der, 0)
    _, tbs, _ = _tlv(certificate, 0)
    found: dict[bytes, bytes] = {}
    for tag, content in _elements(tbs):
        if tag != _TAG_EXTENSIONS:
            continue
        _, listed, _ = _tlv(content, 0)
        for _, extension in _elements(listed):
            parts = _elements(extension)
            if len(parts) < 2 or parts[0][0] != _TAG_OID or parts[-1][0] != _TAG_OCTET_STRING:  # noqa: PLR2004
                raise TransportError("the peer's certificate is malformed")
            found[parts[0][1]] = parts[-1][1]
    return found


def _refuse_a_signing_leaf(der: bytes) -> None:
    """Refuse a leaf that is a certificate authority.

    An X509-SVID leaf sets CA to false and may not sign certificates or
    revocation lists. A workload certificate that could would be an
    authority any admitted peer could use to mint further identities.
    """
    extensions = _extensions(der)

    constraints = extensions.get(_OID_BASIC_CONSTRAINTS)
    if constraints is not None:
        _, body, _ = _tlv(constraints, 0)
        parts = _elements(body)
        if parts and parts[0][0] == _TAG_BOOLEAN and parts[0][1] != b"\x00":
            raise TransportError(
                "the peer's certificate is a certificate authority, not a workload identity"
            )

    usage = extensions.get(_OID_KEY_USAGE)
    if usage is not None:
        tag, body, _ = _tlv(usage, 0)
        if tag != _TAG_BIT_STRING or len(body) < 2:  # noqa: PLR2004
            raise TransportError("the peer's certificate is malformed")
        if body[1] & (_KEY_CERT_SIGN | _CRL_SIGN):
            raise TransportError(
                "the peer's certificate may sign certificates, which a workload identity may not"
            )


class TransportError(Exception):
    """A configuration that cannot be loaded, or a peer that is not admitted.

    It names the identity it refused, because "TLS handshake failed" is the
    error people spend afternoons on. It never contains a key or anything
    read from one.
    """


@dataclass(frozen=True, slots=True)
class Peer:
    """An account. Never an address: an address resolves to whoever holds it today."""

    namespace: str
    service_account: str

    def __str__(self) -> str:
        """Render as the identity reads, so a log line can be searched for."""
        return f"{self.namespace}/{self.service_account}"


class Identity:
    """A loaded workload identity: what to present, and who to admit."""

    def __init__(  # noqa: PLR0913 (every one of them is a separate decision)
        self,
        *,
        mode: Mode,
        cert_file: str,
        key_file: str,
        ca_file: str,
        trust_domain: str,
        peers: Iterable[Peer] = (),
    ) -> None:
        """Hold the paths. Nothing is read until a context is asked for."""
        self.mode: Mode = mode
        self._cert_file = cert_file
        self._key_file = key_file
        self._ca_file = ca_file
        self._trust_domain = trust_domain
        self._peers = frozenset(peers)

    @property
    def peers(self) -> frozenset[Peer]:
        """Who may call. Empty admits nobody, which is the right default."""
        return self._peers

    def client_context(self) -> ssl.SSLContext:
        """Build a context for connections this service MAKES.

        Built fresh on every call, and that is the point. A platform rotates
        these on an hour or less; a process that held the context it built at
        start-up authenticates fine until the certificate expires and then
        fails everywhere at once, with an error about expiry and nothing
        pointing at the line that read it. Building per connection is what
        Python makes natural, and it is why a client needs no reload
        machinery at all.

        Host name checking is OFF, and that is a decision rather than an
        oversight: a platform's workload certificate carries no name. It
        carries an identity, which is the stronger statement. "I reached the
        address I meant to" is a weaker question than "the thing answering is
        the account I was told to trust", and the chain is still verified.
        The second question is asked by :meth:`verify_peer`, which the caller
        MUST call once the connection is up.
        """
        context = ssl.create_default_context(ssl.Purpose.SERVER_AUTH, cafile=self._ca_file)
        context.minimum_version = _MINIMUM_VERSION
        context.check_hostname = False
        context.verify_mode = ssl.CERT_REQUIRED
        context.load_cert_chain(self._cert_file, self._key_file)
        return context

    def server_context(self) -> ssl.SSLContext:
        """Build a context for this service's own listener.

        A certificate is REQUIRED from every caller, so a connection without
        one never reaches the application. Which accounts are admitted is
        still :meth:`verify_peer`'s question, asked once the handshake is
        done.

        A server built on this reads its certificate when the context is
        built. Rotation is therefore the awkward case in Python and is not
        solved here: a process-per-worker server recycles its workers within
        the certificate's lifetime, which is ordinary and needs no new
        machinery, and anything else sits behind the terminating proxy. See
        docs/canon/python.md.
        """
        context = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH, cafile=self._ca_file)
        context.minimum_version = _MINIMUM_VERSION
        context.verify_mode = ssl.CERT_REQUIRED
        context.load_cert_chain(self._cert_file, self._key_file)
        return context

    def peer_of(self, certificate: dict[str, Any] | None, *, der: bytes | None = None) -> Peer:
        """Read the account out of a peer certificate's identity.

        Takes what ``SSLSocket.getpeercert()`` returns. Anything that is not
        the expected shape is refused rather than guessed at, because a
        partial match here is an identity nobody meant to grant.

        A workload certificate carries exactly ONE URI name, of any scheme,
        and is not a certificate authority. The first is read from the
        dictionary; the second is not in it, so pass what
        ``SSLSocket.getpeercert(binary_form=True)`` returns as ``der`` and it
        is checked too. :meth:`verify_peer` takes the same argument.
        """
        if not certificate:
            raise TransportError("the peer presented no certificate")

        if der is not None:
            _refuse_a_signing_leaf(der)

        uris = [value for kind, value in certificate.get("subjectAltName", ()) if kind == "URI"]
        if not uris:
            raise TransportError("the peer's certificate carries no workload identity")
        if len(uris) > 1:
            # Two identities in one certificate is not a peer to guess about.
            raise TransportError(f"the peer's certificate carries {len(uris)} identities")
        if not uris[0].startswith(f"{_SCHEME}://"):
            raise TransportError(f"identity {uris[0]} is not in the shape this service reads")

        parsed = urlsplit(uris[0])
        if self._trust_domain and parsed.netloc != self._trust_domain:
            raise TransportError(
                f"identity {uris[0]} is from trust domain {parsed.netloc!r}, "
                f"not {self._trust_domain!r}",
            )

        parts = parsed.path.strip("/").split("/")
        if len(parts) != _PATH_LENGTH or parts[0] != "ns" or parts[2] != "sa":
            raise TransportError(f"identity {uris[0]} is not in the shape this service reads")
        return Peer(namespace=parts[1], service_account=parts[3])

    def verify_peer(self, certificate: dict[str, Any] | None, *, der: bytes | None = None) -> Peer:
        """Admit the peer, or raise saying which account was refused.

        Call this once a connection is up and before anything is read or
        written on it. The chain is already verified by then — OpenSSL did
        that during the handshake — and this is the other half: WHO the
        verified certificate belongs to. Pass ``getpeercert(binary_form=True)``
        as ``der`` so a leaf that is a certificate authority is refused too.
        """
        peer = self.peer_of(certificate, der=der)
        if peer not in self._peers:
            raise TransportError(f"refused a peer: {peer} is not on this service's list")
        return peer


def load(cfg: dict[str, Any] | None) -> Identity | None:
    """Read the ``tls`` fragment, or return None when there is nothing to load.

    None means "serve cleartext", not an error. `off` is the default and
    always will be: a chart is installable by someone whose platform
    provides none of this, and a default that assumed one would produce a
    service waiting forever for a volume nobody serves.
    """
    if not cfg:
        return None
    mode = cfg.get("mode", "off")
    if mode == "off":
        return None
    if mode not in ("permissive", "strict"):
        raise TransportError(f"tls.mode {mode!r} is not one this service knows")

    missing = [key for key in ("certFile", "keyFile", "caFile") if not cfg.get(key)]
    if missing:
        raise TransportError(
            f"tls.mode is {mode} but {', '.join(missing)} "
            f"{'is' if len(missing) == 1 else 'are'} not set: "
            "the platform mounts the identity, and this is where it says where",
        )
    for key in ("certFile", "keyFile", "caFile"):
        if not Path(cfg[key]).is_file():
            raise TransportError(f"tls.{key} is {cfg[key]!r}, which is not a file")
    if not cfg.get("trustDomain"):
        # Without it a peer from ANY trust domain is admitted by account
        # name alone, which is a different service's `default` looking
        # exactly like this one's.
        raise TransportError(
            "tls.trustDomain is required once tls.mode is not off: "
            "without it a peer from any trust domain is admitted",
        )

    return Identity(
        mode=mode,
        cert_file=cfg["certFile"],
        key_file=cfg["keyFile"],
        ca_file=cfg["caFile"],
        trust_domain=cfg["trustDomain"],
        peers=[
            Peer(namespace=p["namespace"], service_account=p["serviceAccount"])
            for p in cfg.get("peers", [])
        ],
    )
