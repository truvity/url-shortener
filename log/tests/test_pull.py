"""An idle stream is an answer, not a crash."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest
from nats.errors import TimeoutError as NATSTimeoutError

from url_shortener_log.pull import pull

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
