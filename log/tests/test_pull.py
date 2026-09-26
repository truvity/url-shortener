"""An idle stream is an answer, not a crash."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from nats.errors import ConnectionClosedError
from nats.errors import TimeoutError as NATSTimeoutError

from url_shortener_log.pull import pull, pull_or_reconnect

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
