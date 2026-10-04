"""An idle stream is an answer, not a crash."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from nats.errors import ConnectionClosedError
from nats.errors import TimeoutError as NATSTimeoutError

from url_shortener_log.pull import ack_all, pull, pull_or_reconnect

if TYPE_CHECKING:
    from nats.aio.msg import Msg


class Idle:
    """A stream on which the fetch raises what the client raises."""

    def __init__(self, error: BaseException) -> None:
        """Take the error to raise."""
        self._error = error

    async def fetch(self, batch: int, timeout: float) -> list[Msg]:  # noqa: ASYNC109, ARG002
        """Raise, as an idle fetch does."""
        raise self._error


class Busy:
    """A stream with something on it."""

    async def fetch(self, batch: int, timeout: float) -> list[Msg]:  # noqa: ASYNC109, ARG002
        """Return two messages."""
        return ["a", "b"]  # type: ignore[list-item]


@pytest.mark.parametrize(
    "error",
    [TimeoutError(), NATSTimeoutError()],
    ids=["the standard class the fetch path really raises", "the client's own subclass"],
)
async def test_nothing_arriving_is_an_empty_batch(error: BaseException) -> None:
    assert await pull(Idle(error), batch=10, timeout=1) == []


async def test_what_arrived_is_returned() -> None:
    assert len(await pull(Busy(), batch=10, timeout=1)) == 2


async def test_any_other_failure_still_surfaces() -> None:
    with pytest.raises(ConnectionError):
        await pull(Idle(ConnectionError("gone")), batch=10, timeout=1)


class Gone:
    """A subscription on a connection the server has already closed for good.

    Every `fetch` raises `ConnectionClosedError`, the same as the real
    client once `nats-py` has given up on the connection — over a rotated
    credential it never retries this on its own. Standing in for the whole
    scenario measured live: a credential file changes under a long-running
    connection, the broker drops that connection, and the client's `fetch`
    starts raising this forever.
    """

    async def fetch(self, batch: int, timeout: float) -> list[Msg]:  # noqa: ASYNC109, ARG002
        """Raise, as a fetch on a permanently closed connection does."""
        raise ConnectionClosedError


async def test_a_closed_connection_is_recovered_by_dialing_again() -> None:
    # The reconnect reads whatever credential is current now -- a fresh
    # subscription that works, standing in for a dial made after the token
    # file was refreshed -- and the caller ends up with THAT subscription,
    # not the dead one.
    dials = 0

    async def reconnect() -> Busy:
        nonlocal dials
        dials += 1
        return Busy()

    subscription, messages = await pull_or_reconnect(Gone(), reconnect, batch=10, timeout=1)

    assert dials == 1
    assert isinstance(subscription, Busy)
    assert len(messages) == 2


async def test_a_connection_still_closed_after_dialing_again_surfaces() -> None:
    # Two dials in a row failing is no longer "the credential rotated" --
    # something else is wrong, and that is for Kubernetes to restart on,
    # not a loop to spin in forever.
    async def reconnect() -> Gone:
        return Gone()

    with pytest.raises(ConnectionClosedError):
        await pull_or_reconnect(Gone(), reconnect, batch=10, timeout=1)


async def test_an_ordinary_idle_fetch_never_reconnects() -> None:
    async def reconnect() -> Busy:
        pytest.fail("an idle stream is not a closed connection")

    subscription, messages = await pull_or_reconnect(
        Idle(NATSTimeoutError()), reconnect, batch=10, timeout=1
    )

    assert isinstance(subscription, Idle)
    assert messages == []


class Settled:
    """A message whose acknowledgement works, or fails as a closed connection does."""

    def __init__(self, *, closed: bool = False) -> None:
        """Say whether the connection is closed under it."""
        self._closed = closed
        self.acked = 0

    async def ack(self) -> None:
        """Acknowledge, or raise as `nats-py` does on a closed connection."""
        if self._closed:
            raise ConnectionClosedError
        self.acked += 1


async def test_acknowledging_on_a_closed_connection_does_not_raise() -> None:
    first, second, third = Settled(), Settled(closed=True), Settled(closed=True)

    count = await ack_all([first, second, third])  # type: ignore[list-item]

    assert count == 1
    assert first.acked == 1


async def test_every_message_is_acknowledged_on_a_healthy_connection() -> None:
    messages = [Settled(), Settled()]

    assert await ack_all(messages) == 2  # type: ignore[arg-type]


async def test_a_closed_ack_then_a_closed_fetch_reconnects_and_carries_on() -> None:
    # The sequence from prod: the flush archives, the ack finds the
    # connection closed, and the next fetch finds it closed too. The loop
    # must end up on a working subscription, not out of the process.
    assert await ack_all([Settled(closed=True)]) == 0  # type: ignore[list-item]

    dials = 0

    async def reconnect() -> Busy:
        nonlocal dials
        dials += 1
        return Busy()

    subscription, messages = await pull_or_reconnect(Gone(), reconnect, batch=10, timeout=1)

    assert dials == 1
    assert isinstance(subscription, Busy)
    assert len(messages) == 2
