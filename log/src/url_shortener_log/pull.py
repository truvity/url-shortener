"""Pull a batch from the stream, treating "nothing arrived" as an answer."""

from __future__ import annotations

from typing import TYPE_CHECKING, Protocol

if TYPE_CHECKING:
    from nats.aio.msg import Msg


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
