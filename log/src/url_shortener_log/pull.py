"""Pull a batch from the stream, treating "nothing arrived" as an answer."""

from __future__ import annotations

from typing import TYPE_CHECKING, Protocol

from nats.errors import ConnectionClosedError

if TYPE_CHECKING:
    from collections.abc import Awaitable, Callable

    from nats.aio.msg import Msg

    # A fresh dial, called only once the old connection is beyond saving.
    # Reads whatever credential is current NOW, which is the reason to call
    # it again rather than retry the same subscription.
    Reconnect = Callable[[], Awaitable["Subscription"]]


class Subscription(Protocol):
    """The one call this component makes against a pull subscription."""

    async def fetch(self, batch: int, timeout: float) -> list[Msg]:  # noqa: ASYNC109 — the client's own signature
        """Pull up to `batch` messages, waiting at most `timeout` seconds."""
        ...


async def pull(subscription: Subscription, batch: int, timeout: float) -> list[Msg]:  # noqa: ASYNC109
    """Return what arrived, or an empty list when nothing did.

    An idle stream is the ordinary case, and the client reports it as an
    exception. Which one is the trap: its own `nats.errors.TimeoutError` is a
    subclass of the standard one, and the fetch path raises the STANDARD
    class, so a handler naming the client's class catches nothing at the one
    moment it matters. The process died on the first quiet second and the
    orchestrator restarted it, which looked like a flaky pod and was a loop
    that ran whenever there was no traffic.

    The base class covers both, so nothing about the client's choice of
    which to raise can bring this back.
    """
    try:
        return await subscription.fetch(batch=batch, timeout=timeout)
    except TimeoutError:
        return []


async def pull_or_reconnect(
    subscription: Subscription,
    reconnect: Reconnect,
    batch: int,
    timeout: float,  # noqa: ASYNC109 — the client's own signature
) -> tuple[Subscription, list[Msg]]:
    """Fetch a batch, dialing once more if the connection is gone for good.

    A short-lived credential expiring mid-connection is an ordinary event —
    the platform rotates it well before it expires — and every other
    component treats it as one: it is the ONE server error `nats-py` (unlike
    the Go and Kotlin clients on the same broker) does not hand to its own
    reconnect logic. On `-ERR 'Authorization Violation'` it closes the
    client outright, `max_reconnect_attempts` and `allow_reconnect` are
    never consulted, and every subsequent `fetch` raises
    `ConnectionClosedError` forever — nothing brings the connection back on
    its own.

    So this supplies the missing half: on that one error, and only that
    one, dial again — which reads the credential file fresh — and retry the
    fetch once, against the new subscription. A second failure is not
    retried again here; it surfaces, because two dials in a row failing
    is no longer "the credential rotated."
    """
    try:
        return subscription, await pull(subscription, batch=batch, timeout=timeout)
    except ConnectionClosedError:
        subscription = await reconnect()
        return subscription, await pull(subscription, batch=batch, timeout=timeout)
