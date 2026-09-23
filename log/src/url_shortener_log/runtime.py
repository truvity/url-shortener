"""What every component owes the platform: a logger, probes, and a version.

The Go components have an `internal/runtime` package holding exactly these
three things, and this is its counterpart. Keeping the names and the shapes
aligned is not tidiness — it is what makes "the contracts are the same in
every language" a claim a reader can check by opening two files.
"""

from __future__ import annotations

import logging
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as package_version
from typing import TYPE_CHECKING, cast

import structlog

if TYPE_CHECKING:
    from collections.abc import Callable

LIVE_PATH = "/health/live"
READY_PATH = "/health/ready"

# What a build stamps. `version` comes from the installed distribution, which
# the image's build sets; a checkout reports the placeholder, and that is
# correct — a checkout is not a release.
UNKNOWN = "unknown"


def logger(level: str) -> structlog.stdlib.BoundLogger:
    """Configure logging: JSON, to stderr, at one level.

    stderr rather than stdout because stdout is the program's product and
    stderr is its commentary. This program's product is objects in a bucket,
    so it writes nothing to stdout at all.
    """
    parsed = logging.getLevelNamesMapping().get(level.upper())
    if parsed is None:
        msg = f"log level {level!r} is not one this component knows"
        raise ValueError(msg)

    logging.basicConfig(format="%(message)s", stream=sys.stderr, level=parsed)
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso", utc=True),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(parsed),
        logger_factory=structlog.PrintLoggerFactory(file=sys.stderr),
        cache_logger_on_first_use=True,
    )
    return cast("structlog.stdlib.BoundLogger", structlog.get_logger())


def version() -> tuple[str, str]:
    """Return what this build is, as far as it can tell."""
    try:
        return package_version("url-shortener-log"), UNKNOWN
    except PackageNotFoundError:
        return UNKNOWN, UNKNOWN


class Probes:
    """The liveness and readiness listener, on its own port.

    Its own port, because the port that serves traffic is the port an
    ingress, a mesh or a curl can reach, and a readiness endpoint reachable
    from outside the cluster is one anybody can use to take a service out of
    rotation. This component serves no traffic at all, and still owes the
    platform this.

    Liveness answers whether the process is running. It checks NOTHING: a
    liveness probe that checks a dependency restarts a healthy process
    because something else is down, which turns one outage into two.
    """

    def __init__(self, address: str, ready: Callable[[], str | None]) -> None:
        """Bind, but do not serve yet. `ready` returns None when ready."""
        host, _, port = address.rpartition(":")
        self.check = ready
        self._server = ThreadingHTTPServer((host, int(port)), self._handler())
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        probes = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            # The name http.server dispatches on, so it is not ours to choose.
            def do_GET(self) -> None:
                if self.path == LIVE_PATH:
                    self._say(200, b"ok")
                    return
                if self.path == READY_PATH:
                    detail = probes.check()
                    if detail is None:
                        self._say(200, b"ok")
                    else:
                        self._say(503, detail.encode())
                    return
                self._say(404, b"not found")

            def _say(self, code: int, body: bytes) -> None:
                self.send_response(code)
                self.send_header("Content-Type", "text/plain; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, format: str, *args: object) -> None:  # noqa: A002
                """Say nothing. A probe every five seconds is not news."""

        return Handler

    def start(self) -> None:
        """Begin answering."""
        self._thread.start()

    def stop(self) -> None:
        """Stop answering and release the port."""
        self._server.shutdown()
        self._server.server_close()
