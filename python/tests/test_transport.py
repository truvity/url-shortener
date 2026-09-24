"""What the transport helper presents, and who it admits.

The interesting tests are the last two: a real handshake between two
processes' worth of context, where an admitted account gets through and an
account holding a genuine certificate from the same authority does not.
Issuing identities correctly while admitting anyone who holds one is the
failure that looks like success from every other angle.
"""

from __future__ import annotations

import socket
import ssl
import subprocess
import threading
from typing import TYPE_CHECKING, Any

import pytest

from truvity_policy.transport import Identity, Peer, TransportError, load

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

TRUST_DOMAIN = "policy.test"


def identity_uri(namespace: str, account: str) -> str:
    return f"spiffe://{TRUST_DOMAIN}/ns/{namespace}/sa/{account}"


def peer_cert(*uris: str) -> dict[str, Any]:
    """Build what SSLSocket.getpeercert() returns, in the shape this code reads."""
    return {"subjectAltName": tuple(("URI", uri) for uri in uris)}


# --- the identity, read out of a certificate -------------------------------


def make(peers: list[Peer] | None = None, *, trust_domain: str = TRUST_DOMAIN) -> Identity:
    return Identity(
        mode="strict",
        cert_file="",
        key_file="",
        ca_file="",
        trust_domain=trust_domain,
        peers=peers or [],
    )


def test_an_identity_is_read_as_a_namespace_and_an_account() -> None:
    peer = make().peer_of(peer_cert(identity_uri("shortener", "redirect")))
    assert peer == Peer(namespace="shortener", service_account="redirect")
    assert str(peer) == "shortener/redirect"


def test_a_peer_with_no_certificate_is_refused() -> None:
    with pytest.raises(TransportError, match="no certificate"):
        make().peer_of(None)


def test_a_certificate_with_no_workload_identity_is_refused() -> None:
    with pytest.raises(TransportError, match="no workload identity"):
        make().peer_of({"subjectAltName": (("DNS", "redirect.shortener.svc"),)})


def test_two_identities_in_one_certificate_are_not_guessed_between() -> None:
    doubled = peer_cert(identity_uri("a", "one"), identity_uri("b", "two"))
    with pytest.raises(TransportError, match="2 identities"):
        make().peer_of(doubled)


def test_another_trust_domain_is_refused_before_the_account_is_considered() -> None:
    # The account name is the SAME. Without the trust domain check, another
    # cluster's `default` is this cluster's `default`.
    other = "spiffe://somewhere.else/ns/shortener/sa/redirect"
    with pytest.raises(TransportError, match="trust domain"):
        make([Peer("shortener", "redirect")]).peer_of(peer_cert(other))


@pytest.mark.parametrize(
    "uri",
    [
        f"spiffe://{TRUST_DOMAIN}/shortener/redirect",
        f"spiffe://{TRUST_DOMAIN}/ns/shortener",
        f"spiffe://{TRUST_DOMAIN}/namespace/shortener/sa/redirect",
        f"spiffe://{TRUST_DOMAIN}/ns/shortener/account/redirect",
    ],
)
def test_a_shape_this_service_does_not_read_is_refused_rather_than_guessed(uri: str) -> None:
    with pytest.raises(TransportError, match="not in the shape"):
        make().peer_of(peer_cert(uri))


def test_an_empty_list_admits_nobody() -> None:
    # The right default for a service nobody has been granted.
    with pytest.raises(TransportError, match="not on this service's list"):
        make().verify_peer(peer_cert(identity_uri("shortener", "redirect")))


def test_a_listed_account_is_admitted_and_an_unlisted_one_is_not() -> None:
    identity = make([Peer("shortener", "redirect")])
    assert identity.verify_peer(peer_cert(identity_uri("shortener", "redirect"))).namespace == (
        "shortener"
    )
    with pytest.raises(TransportError, match="shortener/stat"):
        identity.verify_peer(peer_cert(identity_uri("shortener", "stat")))


def test_the_same_account_in_another_namespace_is_a_different_account() -> None:
    identity = make([Peer("shortener", "redirect")])
    with pytest.raises(TransportError, match="other/redirect"):
        identity.verify_peer(peer_cert(identity_uri("other", "redirect")))


# --- loading the fragment --------------------------------------------------


def test_off_loads_nothing_which_is_not_an_error() -> None:
    # A chart's default is off, and it must produce a service that runs.
    assert load(None) is None
    assert load({}) is None
    assert load({"mode": "off"}) is None


def test_a_mode_this_service_does_not_know_is_refused() -> None:
    with pytest.raises(TransportError, match=r"tls\.mode"):
        load({"mode": "mutual"})


def test_a_mode_that_is_not_off_without_the_files_is_refused_by_name() -> None:
    with pytest.raises(TransportError, match=r"certFile, keyFile, caFile"):
        load({"mode": "strict"})


def test_a_file_the_platform_did_not_mount_is_refused_by_path(tmp_path: Path) -> None:
    with pytest.raises(TransportError, match="not a file"):
        load(
            {
                "mode": "strict",
                "certFile": str(tmp_path / "tls.crt"),
                "keyFile": str(tmp_path / "tls.key"),
                "caFile": str(tmp_path / "ca.crt"),
            },
        )


# --- a real handshake ------------------------------------------------------


def openssl(*args: str) -> None:
    subprocess.run(["openssl", *args], check=True, capture_output=True)  # noqa: S603, S607


@pytest.fixture(scope="module")
def authority(tmp_path_factory: pytest.TempPathFactory) -> Path:
    """Mint an authority and three identities under it, the way a platform would."""
    root = tmp_path_factory.mktemp("pki")
    # keyUsage is not optional on the authority: OpenSSL refuses a chain
    # whose root does not say it may sign certificates, and the refusal
    # ("CA cert does not include key usage extension") names the root rather
    # than the leaf, which is a confusing afternoon if it happens for real.
    openssl(
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-days",
        "1",
        "-subj",
        "/CN=test authority",
        "-addext",
        "basicConstraints=critical,CA:TRUE",
        "-addext",
        "keyUsage=critical,keyCertSign,cRLSign",
        "-keyout",
        str(root / "ca.key"),
        "-out",
        str(root / "ca.crt"),
    )
    for name, uri in [
        ("server", identity_uri("shortener", "archive")),
        ("admitted", identity_uri("shortener", "redirect")),
        # A REAL certificate from the same authority, for an account nobody
        # granted. This is the one that matters.
        ("stranger", identity_uri("shortener", "stat")),
    ]:
        extension = root / f"{name}.ext"
        # One certificate used both ways, which is what a platform mounts:
        # the same identity is presented when the workload calls and when it
        # is called.
        extension.write_text(
            f"subjectAltName=URI:{uri}\n"
            "basicConstraints=critical,CA:FALSE\n"
            "keyUsage=critical,digitalSignature,keyEncipherment\n"
            "extendedKeyUsage=serverAuth,clientAuth\n",
            encoding="utf-8",
        )
        openssl(
            "req",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-subj",
            f"/CN={name}",
            "-keyout",
            str(root / f"{name}.key"),
            "-out",
            str(root / f"{name}.csr"),
        )
        openssl(
            "x509",
            "-req",
            "-in",
            str(root / f"{name}.csr"),
            "-days",
            "1",
            "-CA",
            str(root / "ca.crt"),
            "-CAkey",
            str(root / "ca.key"),
            "-CAcreateserial",
            "-extfile",
            str(extension),
            "-out",
            str(root / f"{name}.crt"),
        )
    return root


def fragment(root: Path, name: str, peers: list[dict[str, str]]) -> dict[str, Any]:
    return {
        "mode": "strict",
        "certFile": str(root / f"{name}.crt"),
        "keyFile": str(root / f"{name}.key"),
        "caFile": str(root / "ca.crt"),
        "trustDomain": TRUST_DOMAIN,
        "peers": peers,
    }


@pytest.fixture
def server(authority: Path) -> Iterator[tuple[int, list[str]]]:
    """Serve a listener that admits one account, and record what it decided."""
    identity = load(
        fragment(
            authority,
            "server",
            [
                {"namespace": "shortener", "serviceAccount": "redirect"},
            ],
        )
    )
    assert identity is not None
    context = identity.server_context()

    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    decisions: list[str] = []

    def serve() -> None:
        while True:
            try:
                raw, _ = listener.accept()
            except OSError:
                return
            try:
                with context.wrap_socket(raw, server_side=True) as connection:
                    # The chain is verified by now. WHO it belongs to is this.
                    try:
                        peer = identity.verify_peer(connection.getpeercert())
                    except TransportError as refusal:
                        decisions.append(f"refused: {refusal}")
                        connection.send(b"no")
                    else:
                        decisions.append(f"admitted: {peer}")
                        connection.send(b"yes")
            except ssl.SSLError, OSError:
                decisions.append("handshake failed")

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    yield listener.getsockname()[1], decisions
    listener.close()


def call(authority: Path, name: str, port: int) -> bytes:
    identity = load(
        fragment(
            authority,
            name,
            [
                {"namespace": "shortener", "serviceAccount": "archive"},
            ],
        )
    )
    assert identity is not None
    with (
        socket.create_connection(("127.0.0.1", port), timeout=5) as raw,
        identity.client_context().wrap_socket(raw) as connection,
    ):
        # The client checks the SERVER's identity, for the same reason.
        identity.verify_peer(connection.getpeercert())
        return connection.recv(16)


def test_an_admitted_account_gets_through(authority: Path, server: tuple[int, list[str]]) -> None:
    port, decisions = server
    assert call(authority, "admitted", port) == b"yes"
    assert decisions == ["admitted: shortener/redirect"]


def test_a_real_certificate_for_an_unlisted_account_is_refused(
    authority: Path,
    server: tuple[int, list[str]],
) -> None:
    # The stranger's certificate is genuine: same authority, same trust
    # domain, a chain that verifies. Only the ACCOUNT is different. A service
    # that issued identities correctly and admitted everyone holding one
    # would pass every other test in this file.
    port, decisions = server
    assert call(authority, "stranger", port) == b"no"
    assert decisions == ["refused: refused a peer: shortener/stat is not on this service's list"]
